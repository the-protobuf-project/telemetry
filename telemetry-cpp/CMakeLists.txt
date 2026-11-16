cmake_minimum_required(VERSION 3.14...4.5)
project(telemetry-cpp VERSION 1.3.2 LANGUAGES CXX)

# Examples build when this is the project being built, not when another
# project pulls it in with FetchContent or add_subdirectory.
if(CMAKE_SOURCE_DIR STREQUAL PROJECT_SOURCE_DIR)
    set(TELEMETRY_TOP_LEVEL ON)
else()
    set(TELEMETRY_TOP_LEVEL OFF)
endif()

option(TELEMETRY_BUILD_EXAMPLES "Build the examples" ${TELEMETRY_TOP_LEVEL})
option(TELEMETRY_BUILD_TESTS "Build the Catch2 tests (ctest)" OFF)
option(TELEMETRY_FREERTOS "Build for FreeRTOS" OFF)
option(TELEMETRY_USE_OTEL "Export over OTLP through opentelemetry-cpp" ON)
option(TELEMETRY_PROFILING "CPU profiling to Pyroscope (needs libcurl)" ON)
option(TELEMETRY_INSTALL "Install the library and its CMake package" ${TELEMETRY_TOP_LEVEL})

set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
include(FetchContent)
find_package(Threads REQUIRED)

# Each dependency: the installed package when there is one, else fetched at
# the version the Bazel module uses.

if(NOT TELEMETRY_FREERTOS)
    find_package(spdlog 1.17 CONFIG QUIET)
    if(NOT spdlog_FOUND)
        FetchContent_Declare(spdlog
            GIT_REPOSITORY https://github.com/gabime/spdlog.git
            GIT_TAG v1.17.0
            GIT_SHALLOW TRUE)
        set(SPDLOG_INSTALL ${TELEMETRY_INSTALL} CACHE BOOL "" FORCE)
        FetchContent_MakeAvailable(spdlog)
    endif()

    # mcap is header-only, and public: telemetry/mcap/writer.hpp includes it.
    # The writer is built without compression.
    FetchContent_Declare(mcap
        GIT_REPOSITORY https://github.com/foxglove/mcap.git
        GIT_TAG releases/cpp/v2.1.3
        GIT_SHALLOW TRUE)
    FetchContent_GetProperties(mcap)
    if(NOT mcap_POPULATED)
        if(POLICY CMP0169)
            cmake_policy(SET CMP0169 OLD)  # Populate for a repository without its own CMake build
        endif()
        FetchContent_Populate(mcap)
    endif()
    add_library(telemetry_mcap INTERFACE)
    target_include_directories(telemetry_mcap INTERFACE
        $<BUILD_INTERFACE:${mcap_SOURCE_DIR}/cpp/mcap/include>)
endif()

set(TELEMETRY_OTEL_FETCHED OFF)
if(TELEMETRY_USE_OTEL)
    find_package(opentelemetry-cpp CONFIG QUIET)
    if(opentelemetry-cpp_FOUND)
        set(TELEMETRY_OTEL_PREFIX opentelemetry-cpp::)
    else()
        set(WITH_OTLP_HTTP ON CACHE BOOL "" FORCE)
        set(WITH_OTLP_GRPC OFF CACHE BOOL "" FORCE)
        set(BUILD_TESTING OFF CACHE BOOL "" FORCE)
        set(WITH_EXAMPLES OFF CACHE BOOL "" FORCE)
        set(WITH_BENCHMARK OFF CACHE BOOL "" FORCE)
        set(WITH_FUNC_TESTS OFF CACHE BOOL "" FORCE)
        set(OPENTELEMETRY_INSTALL ${TELEMETRY_INSTALL} CACHE BOOL "" FORCE)
        FetchContent_Declare(opentelemetry-cpp
            GIT_REPOSITORY https://github.com/open-telemetry/opentelemetry-cpp.git
            GIT_TAG v1.29.0
            GIT_SHALLOW TRUE)
        FetchContent_MakeAvailable(opentelemetry-cpp)
        set(TELEMETRY_OTEL_PREFIX "")
        set(TELEMETRY_OTEL_FETCHED ON)
    endif()
endif()

if(TELEMETRY_PROFILING AND NOT TELEMETRY_FREERTOS)
    find_package(CURL REQUIRED)
endif()

set(TELEMETRY_SOURCES
    src/telemetry.cpp
    src/logging/logger.cpp
    src/mcap/writer.cpp
    src/metrics/metrics.cpp
    src/profiling/profiler.cpp
    src/tracing/tracer.cpp
)
if(TELEMETRY_USE_OTEL)
    list(APPEND TELEMETRY_SOURCES src/otel/exporter.cpp)
endif()

add_library(telemetry ${TELEMETRY_SOURCES})
add_library(telemetry::telemetry ALIAS telemetry)
target_compile_features(telemetry PUBLIC cxx_std_17)
target_include_directories(telemetry PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>)
target_link_libraries(telemetry PUBLIC Threads::Threads)

if(TELEMETRY_FREERTOS)
    target_compile_definitions(telemetry PUBLIC TELEMETRY_FREERTOS=1)
else()
    target_link_libraries(telemetry PUBLIC spdlog::spdlog $<BUILD_INTERFACE:telemetry_mcap>)
endif()

if(TELEMETRY_USE_OTEL)
    target_compile_definitions(telemetry PUBLIC TELEMETRY_USE_OTEL=1)
    foreach(lib api sdk resources trace logs metrics
                exporter_otlp_http exporter_otlp_http_log exporter_otlp_http_metric)
        target_link_libraries(telemetry PUBLIC ${TELEMETRY_OTEL_PREFIX}opentelemetry_${lib})
    endforeach()
else()
    target_compile_definitions(telemetry PUBLIC TELEMETRY_USE_OTEL=0)
endif()

if(TELEMETRY_PROFILING AND NOT TELEMETRY_FREERTOS)
    target_link_libraries(telemetry PRIVATE CURL::libcurl ${CMAKE_DL_LIBS})
else()
    target_compile_definitions(telemetry PRIVATE TELEMETRY_NO_PROFILING=1)
endif()

if(TELEMETRY_BUILD_EXAMPLES)
    foreach(example main logging logging_mcap metrics tracing loadgen)
        add_executable(telemetry_example_${example} examples/${example}.cpp)
        target_link_libraries(telemetry_example_${example} PRIVATE telemetry)
    endforeach()
endif()

if(TELEMETRY_BUILD_TESTS)
    enable_testing()
    find_package(Catch2 3 CONFIG QUIET)
    if(NOT Catch2_FOUND)
        FetchContent_Declare(Catch2
            GIT_REPOSITORY https://github.com/catchorg/Catch2.git
            GIT_TAG v3.15.1
            GIT_SHALLOW TRUE)
        FetchContent_MakeAvailable(Catch2)
    endif()
    set(TELEMETRY_TESTS tracing_test metrics_test)
    if(TELEMETRY_PROFILING AND NOT TELEMETRY_FREERTOS)
        list(APPEND TELEMETRY_TESTS profiling_test)
    endif()
    foreach(test ${TELEMETRY_TESTS})
        add_executable(${test} tests/${test}.cpp)
        target_link_libraries(${test} PRIVATE telemetry Catch2::Catch2WithMain)
        # Function names in profiles come from the dynamic symbol table.
        set_target_properties(${test} PROPERTIES ENABLE_EXPORTS ON)
        add_test(NAME ${test} COMMAND ${test})
    endforeach()
endif()

if(TELEMETRY_INSTALL)
    include(GNUInstallDirs)
    include(CMakePackageConfigHelpers)

    install(TARGETS telemetry EXPORT telemetry-targets
        LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
        ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
    install(DIRECTORY include/telemetry DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
    if(NOT TELEMETRY_FREERTOS)
        install(DIRECTORY ${mcap_SOURCE_DIR}/cpp/mcap/include/mcap DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
    endif()
    install(EXPORT telemetry-targets
        FILE telemetry-targets.cmake
        NAMESPACE telemetry::
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/telemetry)
    configure_package_config_file(cmake/telemetry-config.cmake.in
        ${CMAKE_CURRENT_BINARY_DIR}/telemetry-config.cmake
        INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/telemetry)
    write_basic_package_version_file(${CMAKE_CURRENT_BINARY_DIR}/telemetry-config-version.cmake
        COMPATIBILITY SameMajorVersion)
    install(FILES
        ${CMAKE_CURRENT_BINARY_DIR}/telemetry-config.cmake
        ${CMAKE_CURRENT_BINARY_DIR}/telemetry-config-version.cmake
        DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/telemetry)
endif()
