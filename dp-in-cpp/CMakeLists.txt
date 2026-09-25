cmake_minimum_required(VERSION 3.12)
project(DesignPatternsInCpp LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

set(FETCHCONTENT_BASE_DIR "$ENV{HOME}/_deps" CACHE PATH "Base directory for FetchContent downloads")

include(CTest)

# 打包目标：静态库 libdesign_patterns.a
add_library(design_patterns STATIC creational/checksum_service.cpp)
target_include_directories(design_patterns PUBLIC ${CMAKE_CURRENT_SOURCE_DIR})

if (BUILD_TESTING)
  include(FetchContent)
  FetchContent_Declare(
    googletest
    GIT_REPOSITORY https://github.com/google/googletest.git
    GIT_TAG        v1.15.0
  )
  FetchContent_MakeAvailable(googletest)

  add_executable(test_design_patterns tests/checksum_service_test.cpp)
  target_link_libraries(test_design_patterns design_patterns gtest gtest_main)

  include(GoogleTest)
  gtest_discover_tests(test_design_patterns)
endif()
