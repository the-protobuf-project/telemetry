cmake_minimum_required(VERSION 3.16)
project(telemetry-example VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Option to use local telemetry-cpp or fetch from remote
option(USE_LOCAL_TELEMETRY "Use local telemetry-cpp directory" ON)

if(USE_LOCAL_TELEMETRY)
    # Add local telemetry-cpp as subdirectory
    add_subdirectory(${CMAKE_CURRENT_SOURCE_DIR}/../telemetry-cpp ${CMAKE_BINARY_DIR}/telemetry-cpp)
else()
    # Fetch from git (for CI/CD or when not developing locally)
    include(FetchContent)
    FetchContent_Declare(
        telemetry-cpp
        GIT_REPOSITORY https://github.com/the-protobuf-project/telemetry.git
        GIT_TAG main
        SOURCE_SUBDIR telemetry-cpp
    )
    FetchContent_MakeAvailable(telemetry-cpp)
endif()

add_executable(chat-service main.cpp)
target_link_libraries(chat-service PRIVATE telemetry)
