cmake_minimum_required(VERSION 3.20)
project(clef CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

# Prebuilt llama.cpp release libraries + headers, fetched by the Decider
# engine build (see ../ggmlc-custom-decider/justfile `deps`).
set(DEPS ${CMAKE_CURRENT_SOURCE_DIR}/../ggmlc-custom-decider/deps)

find_package(Threads REQUIRED)

add_executable(clef main.cpp)
target_include_directories(clef PRIVATE ${DEPS}/include)
target_link_directories(clef PRIVATE ${DEPS}/lib)
target_link_libraries(clef PRIVATE mtmd llama ggml ggml-base Threads::Threads)
target_compile_options(clef PRIVATE -Wall -Wextra -Wno-unused-parameter)
# Find bundled libraries in ./lib next to the executable, like the release layout.
set_target_properties(clef PROPERTIES
  BUILD_RPATH "\$ORIGIN/lib;${DEPS}/lib"
  INSTALL_RPATH "\$ORIGIN/lib")
