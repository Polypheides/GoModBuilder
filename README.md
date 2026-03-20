# Go Mod Builder

A high-performance, unified Go-based mod building tool for Command & Conquer: Generals and Zero Hour.

## Features

- **Windows GUI**: Native Windows interface built with the `walk` library.
- **Embedded Tools**: Support for standalone binaries with embedded tools and icons.
- **Parallel Processing**: Concurrent build engine with a process semaphore to prevent system overload.
- **Recursive Wildcards**: `**` globbing support that mirrors source directory structures.
- **Texture Support**: Integrated **Crunch** (v1.04) for DDS (DXT1/DXT5) compression.
- **Game Text Toolset**: CSF compilation/decompilation with automated language extraction.
- **Text Filtering**: `excludeMarkersList` support for Core/Optional content logic.
- **Installation Management**:
    - **Snapshot System**: Baseline capture for clean mod uninstallation.
    - **Registry Integration**: Auto-discovery of game paths and registry-based language syncing.
    - **Safe-Backup**: Automatic creation and restoration of `.bak` files for vanilla assets.
- **Release Packaging**: Integrated 7-Zip (`mx9`) zipping with automated MD5/SHA256 hash generation.
- **Automation Hooks**: Python-based event hooks and automated changelog generation.
- **Setup Script**: `gen.go` script for verified tool acquisition and source-to-binary compilation.

## Getting Started

### Prerequisites

- [Go 1.26+](https://golang.org/dl/)
- Git

### Compilation

To build the standalone executable with the embedded manifest and optimized modding tools, follow these steps:

#### 1. Automated Tool Preparation
The project includes a `gen.go` script that automates tool dependency management. It will download/verify all required binaries, generate Windows resources (icons/manifests), and even build `gametextcompiler.exe` from source if `cmake` is available. It automatically cleans up after itself to keep the repo pristine:
```powershell
# This script sets up internal/bin and generates rsrc.syso
go run scripts/gen.go
```

#### 3. Final Binary Build
Compile the optimized Go binary with all tools embedded for a standalone experience:
```powershell
# To build the GUI with embedded tools:
go build -tags embed -o GoModBuilder.exe .
```

To build without embedding (tools will pull at runtime):
```powershell
go build -o GoModBuilder.exe .
```
> [!TIP]
> After building, you can run `.\GoModBuilder.exe --setup` to perform a final verification of all external dependencies.

## Usage

### 1. GUI Mode (Default)
Simply **double-click** `GoModBuilder.exe` to launch the native Windows GUI.
- The terminal remains visible for real-time build logs and tool output.
- The window is compact and resizable to fit your workflow.

### 2. CLI Mode (Automation)
Run the tool from a terminal for automated builds:
```powershell
# Setup required tools
.\GoModBuilder.exe --setup

# Build a specific pack
.\GoModBuilder.exe --build --pack "CoreArabic" --verbose

# Install mod to game directory
.\GoModBuilder.exe --install --target "C:\Games\Generals Zero Hour"
```

## Project Structure

- `main.go`: Entry point and CLI flag handling.
- `internal/gui.go`: Native Windows GUI implementation.
- `internal/builder.go`: Core build, installation, and release zipping logic.
- `internal/config.go`: Automated JSON configuration discovery and merging.
- `internal/tools.go`: Tool runner, download, and dependency management.
- `internal/bin/`: Location for downloaded/embedded tools.

## License

This project is licensed under the 3-Clause BSD License - see the [LICENSE](LICENSE) file for details.
