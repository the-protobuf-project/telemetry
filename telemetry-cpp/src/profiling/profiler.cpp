#include "telemetry/profiling/profiler.hpp"

#include "telemetry/platform.hpp"

// A build may leave sampling out (CMake's TELEMETRY_PROFILING=OFF, which also
// drops libcurl); the Profiler is then one that never runs.
#if TELEMETRY_PLATFORM_POSIX && !defined(TELEMETRY_NO_PROFILING)
#define TELEMETRY_PROFILER_ENABLED 1
#else
#define TELEMETRY_PROFILER_ENABLED 0
#endif

#include <algorithm>
#include <cctype>
#include <cstdio>
#include <cstring>

#if TELEMETRY_PROFILER_ENABLED
#include <curl/curl.h>
#include <cxxabi.h>
#include <dlfcn.h>
#include <execinfo.h>
#include <signal.h>
#include <sys/time.h>

#include <cerrno>
#include <cstdlib>
#endif

namespace telemetry::profiling {

#if TELEMETRY_PROFILER_ENABLED
namespace {

// Samples, written by the SIGPROF handler and read by the profiler's thread.
// The handler may not allocate or lock, so it claims a free slot with one
// compare-and-swap and fills it; a full buffer drops the sample.
constexpr size_t kSlots = 4096;
constexpr int kDepth = 64;
constexpr int kSkip = 2;  // the handler itself, and the signal trampoline

struct Slot {
    std::atomic<int> state{0};  // 0 free, 1 being written, 2 written
    int depth = 0;
    void* pcs[kDepth];
};

Slot g_slots[kSlots];
std::atomic<uint64_t> g_next{0};
std::atomic<bool> g_active{false};

void on_sample(int, siginfo_t*, void*) {
    const int saved = errno;
    Slot& slot = g_slots[g_next.fetch_add(1, std::memory_order_relaxed) % kSlots];
    int expected = 0;
    if (slot.state.compare_exchange_strong(expected, 1, std::memory_order_acquire)) {
        slot.depth = backtrace(slot.pcs, kDepth);
        slot.state.store(2, std::memory_order_release);
    }
    errno = saved;
}

std::string url_encode(const std::string& s) {
    static const char* hex = "0123456789ABCDEF";
    std::string out;
    for (unsigned char c : s) {
        if (std::isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~') {
            out += static_cast<char>(c);
        } else {
            out += '%';
            out += hex[c >> 4];
            out += hex[c & 15];
        }
    }
    return out;
}

size_t discard(char*, size_t size, size_t n, void*) {
    return size * n;
}

struct sigaction g_previous;

}  // namespace
#endif

Profiler::Profiler(const ServiceOptions& service, ProfilingOptions options) : options_(std::move(options)) {
    const std::string environment = environment_to_string(service.environment);
    std::map<std::string, std::string> tags = {
        {"service", service.name}, {"version", service.version}, {"environment", environment}};
    for (const auto& [k, v] : options_.tags) tags[k] = v;
    name_ = service.name + "." + environment + ".cpu{";
    bool first = true;
    for (const auto& [k, v] : tags) {
        name_ += (first ? "" : ",") + k + "=" + v;
        first = false;
    }
    name_ += "}";

#if TELEMETRY_PROFILER_ENABLED
    if (!options_.enabled || options_.sample_hz <= 0) return;
    bool expected = false;
    if (!g_active.compare_exchange_strong(expected, true)) {
        std::fprintf(stderr, "telemetry: profiling is already running in this process; not starting another\n");
        return;
    }
    curl_global_init(CURL_GLOBAL_DEFAULT);
    void* warm[1];
    backtrace(warm, 1);  // loads what backtrace needs now, not inside the handler

    struct sigaction action{};
    action.sa_sigaction = on_sample;
    action.sa_flags = SA_SIGINFO | SA_RESTART;
    sigemptyset(&action.sa_mask);
    sigaction(SIGPROF, &action, &g_previous);

    itimerval timer{};
    timer.it_interval.tv_usec = 1000000 / options_.sample_hz;
    timer.it_value = timer.it_interval;
    setitimer(ITIMER_PROF, &timer, nullptr);

    running_ = true;
    thread_ = std::thread([this] { run(); });
#endif
}

Profiler::~Profiler() {
    stop();
}

void Profiler::stop() {
#if TELEMETRY_PROFILER_ENABLED
    if (!running_) return;
    itimerval off{};
    setitimer(ITIMER_PROF, &off, nullptr);
    sigaction(SIGPROF, &g_previous, nullptr);
    {
        std::lock_guard<std::mutex> lock(mutex_);
        stopping_ = true;
    }
    wake_.notify_all();
    thread_.join();
    running_ = false;
    g_active.store(false);
#endif
}

void Profiler::run() {
    auto from = std::chrono::system_clock::now();
    auto next_upload = std::chrono::steady_clock::now() + options_.upload_interval;
    std::unique_lock<std::mutex> lock(mutex_);
    while (true) {
        // Drain often, so the sample buffer never fills between uploads.
        wake_.wait_for(lock, std::chrono::milliseconds(100), [this] { return stopping_; });
        drain();
        const bool stopping = stopping_;
        if (stopping || std::chrono::steady_clock::now() >= next_upload) {
            const auto until = std::chrono::system_clock::now();
            lock.unlock();
            upload(from, until);
            lock.lock();
            from = until;
            next_upload = std::chrono::steady_clock::now() + options_.upload_interval;
        }
        if (stopping) return;
    }
}

void Profiler::drain() {
#if TELEMETRY_PROFILER_ENABLED
    for (auto& slot : g_slots) {
        if (slot.state.load(std::memory_order_acquire) != 2) continue;
        if (slot.depth > kSkip) {
            // Root first, as folded stacks are written.
            std::vector<void*> stack(slot.pcs + kSkip, slot.pcs + slot.depth);
            std::reverse(stack.begin(), stack.end());
            ++stacks_[std::move(stack)];
        }
        slot.state.store(0, std::memory_order_release);
    }
#endif
}

const std::string& Profiler::symbol(void* pc) {
    auto it = symbols_.find(pc);
    if (it != symbols_.end()) return it->second;
    std::string name;
#if TELEMETRY_PROFILER_ENABLED
    Dl_info info{};
    if (dladdr(pc, &info) && info.dli_sname) {
        int status = 0;
        char* demangled = abi::__cxa_demangle(info.dli_sname, nullptr, nullptr, &status);
        name = status == 0 && demangled ? demangled : info.dli_sname;
        std::free(demangled);
    } else if (info.dli_fname) {
        const char* base = std::strrchr(info.dli_fname, '/');
        char offset[32];
        std::snprintf(offset, sizeof offset, "+0x%zx",
                      static_cast<size_t>(static_cast<char*>(pc) - static_cast<char*>(info.dli_fbase)));
        name = std::string(base ? base + 1 : info.dli_fname) + offset;
    }
#endif
    if (name.empty()) {
        char raw[32];
        std::snprintf(raw, sizeof raw, "%p", pc);
        name = raw;
    }
    std::replace(name.begin(), name.end(), ';', ':');  // ';' separates frames
    return symbols_.emplace(pc, std::move(name)).first->second;
}

std::string Profiler::folded() {
    std::map<std::string, uint64_t> lines;
    for (const auto& [stack, count] : stacks_) {
        std::string line;
        for (void* pc : stack) {
            if (!line.empty()) line += ';';
            line += symbol(pc);
        }
        lines[line] += count;
    }
    std::string out;
    for (const auto& [line, count] : lines) out += line + " " + std::to_string(count) + "\n";
    return out;
}

void Profiler::upload(std::chrono::system_clock::time_point from, std::chrono::system_clock::time_point until) {
#if TELEMETRY_PROFILER_ENABLED
    std::string body;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        body = folded();
        stacks_.clear();
    }
    if (body.empty()) return;
    using std::chrono::duration_cast;
    using std::chrono::seconds;
    const auto url = options_.server_address + "/ingest?name=" + url_encode(name_) +
                     "&from=" + std::to_string(duration_cast<seconds>(from.time_since_epoch()).count()) +
                     "&until=" + std::to_string(duration_cast<seconds>(until.time_since_epoch()).count()) +
                     "&format=folded&sampleRate=" + std::to_string(options_.sample_hz) +
                     "&spyName=telemetry-cpp&units=samples&aggregationType=sum";
    CURL* curl = curl_easy_init();
    if (!curl) {
        failed_.fetch_add(1);
        return;
    }
    curl_slist* headers = curl_slist_append(nullptr, "Content-Type: text/plain");
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, body.c_str());
    curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE, static_cast<long>(body.size()));
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 10L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, discard);
    if (!options_.basic_auth_user.empty()) {
        const auto auth = options_.basic_auth_user + ":" + options_.basic_auth_password;
        curl_easy_setopt(curl, CURLOPT_USERPWD, auth.c_str());
    }
    long status = 0;
    const bool sent = curl_easy_perform(curl) == CURLE_OK &&
                      curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status) == CURLE_OK && status / 100 == 2;
    (sent ? uploads_ : failed_).fetch_add(1);
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);
#else
    (void)from;
    (void)until;
#endif
}

}  // namespace telemetry::profiling
