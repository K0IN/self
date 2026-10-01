cmake_minimum_required(VERSION 3.20)
project(ggmlc-audio CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

# Shares the prebuilt llama.cpp libraries + headers of the Decider engine, so
# both engines run on the exact same libs in the bundle's shared lib/.
set(DEPS ${CMAKE_CURRENT_SOURCE_DIR}/../ggmlc-custom-decider/deps)

add_executable(ggmlc-audio main.cpp)
target_include_directories(ggmlc-audio PRIVATE ${DEPS}/include)
target_link_directories(ggmlc-audio PRIVATE ${DEPS}/lib)
target_link_libraries(ggmlc-audio PRIVATE mtmd llama ggml ggml-base)
target_compile_options(ggmlc-audio PRIVATE -Wall -Wextra -Wno-unused-parameter)
set_target_properties(ggmlc-audio PROPERTIES
  BUILD_RPATH "\$ORIGIN/lib;${DEPS}/lib"
  INSTALL_RPATH "\$ORIGIN/lib")
