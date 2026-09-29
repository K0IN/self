cmake_minimum_required(VERSION 3.20)
project(ggmlc-custom-decider CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

# Prebuilt llama.cpp release libraries + headers (see `just engine deps`).
set(DEPS ${CMAKE_CURRENT_SOURCE_DIR}/deps)

add_executable(ggmlc-custom-decider main.cpp)
target_include_directories(ggmlc-custom-decider PRIVATE ${DEPS}/include)
target_link_directories(ggmlc-custom-decider PRIVATE ${DEPS}/lib)
target_link_libraries(ggmlc-custom-decider PRIVATE mtmd llama ggml ggml-base)
target_compile_options(ggmlc-custom-decider PRIVATE -Wall -Wextra -Wno-unused-parameter)
# Find bundled libraries in ./lib next to the executable, like the release layout.
set_target_properties(ggmlc-custom-decider PROPERTIES
  BUILD_RPATH "\$ORIGIN/lib;${DEPS}/lib"
  INSTALL_RPATH "\$ORIGIN/lib")
