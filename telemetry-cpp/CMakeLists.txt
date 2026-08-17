cmake_minimum_required(VERSION 3.14)
project(telemetry-cpp VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

option(TELEMETRY_BUILD_EXAMPLES "Build examples" ON)
option(TELEMETRY_BUILD_TESTS "Build tests" OFF)
option(TELEMETRY_FREERTOS "Build for FreeRTOS" OFF)
option(TELEMETRY_USE_OTEL "Enable OpenTelemetry support" ON)

include(FetchContent)

if(NOT TELEMETRY_FREERTOS)
    FetchContent_Declare(
        spdlog
        GIT_REPOSITORY https://github.com/gabime/spdlog.git
        GIT_TAG v1.14.1
    )
    FetchContent_MakeAvailable(spdlog)

    FetchContent_Declare(
        mcap
        GIT_REPOSITORY https://github.com/foxglove/mcap.git
        GIT_TAG releases/cpp/v1.4.1
    )
    FetchContent_MakeAvailable(mcap)

    add_library(mcap INTERFACE)
    target_include_directories(mcap INTERFACE
        $<BUILD_INTERFACE:${mcap_SOURCE_DIR}/cpp/mcap/include>
        $<INSTALL_INTERFACE:include>
    )
endif()

set(TELEMETRY_SOURCES
    src/telemetry.cpp
    src/logging/logger.cpp
    src/mcap/writer.cpp
    src/metrics/metrics.cpp
    src/tracing/tracer.cpp
    src/otel/exporter.cpp
)

set(TELEMETRY_HEADERS
    include/telemetry/telemetry.hpp
    include/telemetry/config.hpp
    include/telemetry/platform.hpp
    include/telemetry/logging/logger.hpp
    include/telemetry/logging/formatter.hpp
    include/telemetry/mcap/writer.hpp
    include/telemetry/mcap/schemas.hpp
    include/telemetry/metrics/metrics.hpp
    include/telemetry/tracing/tracer.hpp
    include/telemetry/otel/exporter.hpp
)

add_library(telemetry ${TELEMETRY_SOURCES} ${TELEMETRY_HEADERS})
add_library(telemetry::telemetry ALIAS telemetry)

target_include_directories(telemetry
    PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
        $<INSTALL_INTERFACE:include>
)

if(TELEMETRY_FREERTOS)
    target_compile_definitions(telemetry PUBLIC TELEMETRY_FREERTOS=1)
else()
    target_link_libraries(telemetry
        PUBLIC
            spdlog::spdlog
            mcap
    )
endif()

if(TELEMETRY_USE_OTEL)
    set(WITH_OTLP_HTTP ON CACHE BOOL "Enable OTLP HTTP exporter" FORCE)
    set(WITH_OTLP_GRPC OFF CACHE BOOL "Disable OTLP gRPC exporter" FORCE)
    set(BUILD_TESTING OFF CACHE BOOL "Disable OTel tests" FORCE)
    set(WITH_EXAMPLES OFF CACHE BOOL "Disable OTel examples" FORCE)
    set(WITH_BENCHMARK OFF CACHE BOOL "Disable OTel benchmarks" FORCE)

    FetchContent_Declare(
        opentelemetry-cpp
        GIT_REPOSITORY https://github.com/open-telemetry/opentelemetry-cpp.git
        GIT_TAG v1.18.0
    )
    FetchContent_MakeAvailable(opentelemetry-cpp)

    target_link_libraries(telemetry
        PUBLIC
            opentelemetry_api
            opentelemetry_sdk
            opentelemetry_exporter_otlp_http
            opentelemetry_exporter_otlp_http_log
            opentelemetry_exporter_otlp_http_metric
            opentelemetry_logs
            opentelemetry_trace
            opentelemetry_metrics
    )
    target_compile_definitions(telemetry PUBLIC TELEMETRY_USE_OTEL=1)
endif()

if(TELEMETRY_BUILD_EXAMPLES)
    add_executable(logging_example examples/logging.cpp)
    target_link_libraries(logging_example PRIVATE telemetry)

    add_executable(logging_mcap_example examples/logging_mcap.cpp)
    target_link_libraries(logging_mcap_example PRIVATE telemetry)

    add_executable(metrics_example examples/metrics.cpp)
    target_link_libraries(metrics_example PRIVATE telemetry)

    add_executable(tracing_example examples/tracing.cpp)
    target_link_libraries(tracing_example PRIVATE telemetry)
endif()

if(NOT TELEMETRY_USE_OTEL)
    include(GNUInstallDirs)

    install(TARGETS telemetry spdlog mcap
        EXPORT telemetry-targets
        LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
        ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
    )

    install(DIRECTORY include/telemetry
        DESTINATION ${CMAKE_INSTALL_INCLUDEDIR}
    )

    install(EXPORT telemetry-targets
        FILE telemetry-config.cmake
        NAMESPACE telemetry::
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/telemetry
    )
endif()
