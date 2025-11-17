#!/bin/bash

# Comzy Cross-Platform Build Script
# Builds binaries for all major operating systems and architectures

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
APP_NAME="comzy"
VERSION="1.0.0"
BUILD_DIR="build"
DIST_DIR="dist"

# Print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Clean previous builds
clean() {
    print_info "Cleaning previous builds..."
    rm -rf "$BUILD_DIR"
    rm -rf "$DIST_DIR"
    mkdir -p "$BUILD_DIR"
    mkdir -p "$DIST_DIR"
}

# Build for specific OS and architecture
build() {
    local os=$1
    local arch=$2
    local output_name="${APP_NAME}"
    
    if [ "$os" = "windows" ]; then
        output_name="${APP_NAME}.exe"
    fi
    
    local output_path="${BUILD_DIR}/${APP_NAME}-${os}-${arch}"
    mkdir -p "$output_path"
    
    print_info "Building for ${os}/${arch}..."
    
    GOOS=$os GOARCH=$arch go build -o "${output_path}/${output_name}" \
        -ldflags="-s -w -X main.Version=${VERSION}" \
        main.go
    
    if [ $? -eq 0 ]; then
        print_success "Built ${os}/${arch}"
        
        # Create archive
        cd "$BUILD_DIR"
        if [ "$os" = "windows" ]; then
            zip -q "${APP_NAME}-${os}-${arch}.zip" "${APP_NAME}-${os}-${arch}/${output_name}"
            mv "${APP_NAME}-${os}-${arch}.zip" "../${DIST_DIR}/"
        else
            tar -czf "${APP_NAME}-${os}-${arch}.tar.gz" "${APP_NAME}-${os}-${arch}/${output_name}"
            mv "${APP_NAME}-${os}-${arch}.tar.gz" "../${DIST_DIR}/"
        fi
        cd ..
    else
        print_error "Failed to build ${os}/${arch}"
        return 1
    fi
}

# Main build function
main() {
    print_info "Starting Comzy cross-platform build..."
    print_info "Version: ${VERSION}"
    echo ""
    
    # Clean previous builds
    clean
    
    # Check if go is installed
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed. Please install Go first."
        exit 1
    fi
    
    print_info "Go version: $(go version)"
    echo ""
    
    # Install dependencies
    print_info "Installing dependencies..."
    go mod download
    go mod tidy
    echo ""
    
    # Build for all platforms
    print_info "Building binaries for all platforms..."
    echo ""
    
    # Linux
    build "linux" "amd64"
    build "linux" "arm64"
    build "linux" "arm"
    build "linux" "386"
    
    # macOS
    build "darwin" "amd64"
    build "darwin" "arm64"
    
    # Windows
    build "windows" "amd64"
    build "windows" "arm64"
    build "windows" "386"
    
    # FreeBSD
    build "freebsd" "amd64"
    build "freebsd" "arm64"
    
    # OpenBSD
    build "openbsd" "amd64"
    build "openbsd" "arm64"
    
    echo ""
    print_success "All builds completed!"
    echo ""
    
    # List built files
    print_info "Built packages:"
    ls -lh "$DIST_DIR"
    echo ""
    
    # Calculate total size
    total_size=$(du -sh "$DIST_DIR" | cut -f1)
    print_info "Total size: ${total_size}"
    
    # Generate checksums
    print_info "Generating checksums..."
    cd "$DIST_DIR"
    if command -v shasum &> /dev/null; then
        shasum -a 256 * > checksums.txt
    elif command -v sha256sum &> /dev/null; then
        sha256sum * > checksums.txt
    fi
    cd ..
    
    print_success "Build process complete!"
    print_info "Binaries are available in the '${DIST_DIR}' directory"
}

# Run main function
main
