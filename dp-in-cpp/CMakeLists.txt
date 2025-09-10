cmake_minimum_required(VERSION 3.12)
project(DesignPatternsInCpp LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

set(FETCHCONTENT_BASE_DIR "$ENV{HOME}/_deps" CACHE PATH "Base directory for FetchContent downloads")
include(FetchContent)

include_directories(./)

FetchContent_Declare(
  googletest
  GIT_REPOSITORY https://github.com/google/googletest.git
  GIT_TAG        v1.15.0
)
FetchContent_MakeAvailable(googletest)
find_package(ZLIB REQUIRED)
enable_testing()


add_executable(test_design_patterns 
    creational/checksum_service.cpp
    tests/checksum_service_test.cpp
)

target_link_libraries(test_design_patterns gtest gtest_main ZLIB::ZLIB)

include(GoogleTest)
gtest_discover_tests(test_design_patterns)