// +build android

package libtorrent

// #cgo pkg-config: --static libtorrent-rasterbar openssl
// #cgo linux CXXFLAGS: -I/usr/include/libtorrent -I/opt/android-toolchain-arm/include/libtorrent -I/opt/android-toolchain-arm/include -Wno-deprecated-declarations
// #cgo linux LDFLAGS: -nodefaultlibs -lstdc++ -latomic -ldl -lm -lc -L/opt/android-toolchain-arm/lib
import "C"
