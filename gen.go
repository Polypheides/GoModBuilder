//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"GoModBuilder/internal"
	"encoding/binary"
	"net/http"
	"io"
)

func main() {
	fmt.Println("--- Generals Mod Builder Automation ---")
	
	projectDir, _ := os.Getwd()
	binDir := filepath.Join(projectDir, "internal", "bin")
	os.MkdirAll(binDir, 0755)

	tr := internal.NewToolRunner(binDir, projectDir, func(s string) { fmt.Println(s) })

	fmt.Println("Ensuring rsrc tool...")
	if _, err := exec.LookPath("rsrc"); err != nil {
		fmt.Println(" [..] rsrc not found. Installing via go install...")
		cmd := exec.Command("go", "install", "github.com/akavel/rsrc@latest")
		if err := cmd.Run(); err != nil {
			fmt.Printf(" [!!] Failed to install rsrc: %v. Please install manually: go install github.com/akavel/rsrc@latest\n", err)
		} else {
			fmt.Println(" [OK] rsrc installed successfully.")
		}
	}

	fmt.Println("Ensuring GUI icons...")
	if err := ensureIcons(binDir); err != nil {
		fmt.Printf(" [!!] Icon setup failed: %v. GUI will use default icon.\n", err)
	}

	fmt.Println("Vevifying/Downloading tools...")
	for name, info := range internal.DefaultTools {
		path := filepath.Join(binDir, name)

		// Special case for gametextcompiler: try building from source
		if name == "gametextcompiler.exe" {
			if _, err := os.Stat(path); err != nil {
				fmt.Println(" [..] gametextcompiler.exe missing. Building from source...")
				if err := buildGameTextCompiler(projectDir, binDir); err == nil {
					fmt.Printf(" [OK] %s built from source.\n", name)
					continue
				} else {
					fmt.Printf(" [!!] Failed to build from source: %v. Falling back to download.\n", err)
				}
			}
		}

		if _, err := os.Stat(path); err == nil {
			if tr.VerifyHash(path, info.Hash) {
				fmt.Printf(" [OK] %s\n", name)
				continue
			}
		}
		fmt.Printf(" [..] Downloading %s...\n", name)
		if err := tr.DownloadFileWithVerify(info.URL, path, info.Hash); err != nil {
			fmt.Printf(" [!!] Failed to download %s: %v\n", name, err)
		} else {
			fmt.Printf(" [OK] %s downloaded and verified.\n", name)
		}
	}

	fmt.Println("Generating Windows resources...")
	if _, err := exec.LookPath("rsrc"); err == nil {
		args := []string{"-manifest", "main.manifest", "-o", "rsrc.syso"}
		iconPath := filepath.Join(binDir, "icon.ico")
		if _, err := os.Stat(iconPath); err == nil {
			args = append(args, "-ico", iconPath)
		}
		cmd := exec.Command("rsrc", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf(" [!!] rsrc failed: %v\n%s\n", err, string(output))
		} else {
			fmt.Println(" [OK] rsrc.syso generated.")
		}
	} else {
		fmt.Println(" [!!] rsrc tool not found. Skipping resource generation (rsrc.syso will remain as is).")
		fmt.Println("      Install it with: go install github.com/akavel/rsrc@latest")
	}

	fmt.Println("\nSetup complete!")
	fmt.Println("To build the GUI with embedded tools:")
	fmt.Println("  go build -tags embed -o GoModBuilder.exe .")
	fmt.Println("\nTo build without embedding (tools will pull at runtime):")
	fmt.Println("  go build -o GoModBuilder.exe .")
}

func buildGameTextCompiler(projectDir, binDir string) error {
	thymeDir := filepath.Join(projectDir, ".thyme")
	if _, err := os.Stat(thymeDir); err != nil {
		fmt.Println(" [..] Cloning Thyme repository (shallow, branch: Does-it-have-my-language-Yes-)...")
		cmd := exec.Command("git", "clone", "--depth", "1", "--branch", "Does-it-have-my-language-Yes-", "https://github.com/Polypheides/Thyme", ".thyme")
		cmd.Dir = projectDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git clone failed: %v\n%s", err, string(output))
		}
	}

	tempBuildDir := filepath.Join(projectDir, ".tools", "build_gtc")
	os.MkdirAll(tempBuildDir, 0755)
	
	// Create a mock gitverinfo.c and config.h for baseconfig
	os.MkdirAll(filepath.Join(tempBuildDir, "src"), 0755)
	os.WriteFile(filepath.Join(tempBuildDir, "src", "gitverinfo.c"), []byte(`
const char *GITINFO_VERSION = "1.04.0-minimal";
const char *GITINFO_BRANCH = "minimal";
const char *GITINFO_COMMIT_SHA1 = "DEADBEEF";
const char *GITINFO_COMMIT_DATE = "2026-03-19";
const char *GITINFO_COMMIT_TIME = "08:00:00";
const char *GITINFO_COMMIT_AUTHOR = "MinimalBuild";
`), 0644)
	os.WriteFile(filepath.Join(tempBuildDir, "src", "config.h"), []byte(`
#ifndef STANDALONE_CONFIG_H
#define STANDALONE_CONFIG_H
#define STANDALONE
#define __SANITIZE_ADDRESS__ 1
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#define NOMINMAX
#define HAVE_INTRIN_H 1
#define HAVE__DEBUGBREAK 1
#define HAVE__STRICMP 1
#define HAVE__STRNICMP 1
#define IMPLEMENT_POOL(x) 
#define IMPLEMENT_ABSTRACT_POOL(x)
#define DECLARE_POOL(x)
#define DECLARE_ABSTRACT_POOL(x)
#endif
`), 0644)
	os.WriteFile(filepath.Join(tempBuildDir, "src", "audiomanager.h"), []byte(`
#pragma once
enum AudioAffect { AUDIOAFFECT_MUSIC = 1, AUDIOAFFECT_SOUND = 2, AUDIOAFFECT_3DSOUND = 4, AUDIOAFFECT_SPEECH = 8, AUDIOAFFECT_BASEVOL = 16 };
class AudioManager {
public:
    virtual void Stop_Audio(AudioAffect) = 0;
};
extern AudioManager *g_theAudio;
`), 0644)
	os.WriteFile(filepath.Join(tempBuildDir, "src", "mocks.cpp"), []byte(`
#include "config.h"
#include <filesystem.h>
#include <localfilesystem.h>
#include <archivefilesystem.h>
#include <archivefile.h>
#include <win32localfilesystem.h>
#include <win32bigfilesystem.h>
#include <gametextcommon.h>
#include <file.h>
#include <audiomanager.h>
#include <subsysteminterface.h>
#include <memdynalloc.h>
#include <critsection.h>
#include <stdio.h>
#include <stdarg.h>

// Global pointers for Thyme core
FileSystem *g_theFileSystem = nullptr;
LocalFileSystem *g_theLocalFileSystem = nullptr;
ArchiveFileSystem *g_theArchiveFileSystem = nullptr;
SubsystemInterfaceList *g_theSubsystemList = nullptr;
AudioManager *g_theAudio = nullptr;
DynamicMemoryAllocator *g_dynamicMemoryAllocator = (DynamicMemoryAllocator*)malloc(sizeof(DynamicMemoryAllocator));
SimpleCriticalSectionClass *g_dmaCriticalSection = nullptr;

#ifdef PLATFORM_WINDOWS
#include <windows.h>
extern "C" HWND g_applicationHWnd = NULL;
#endif

// Dynamic Memory Allocator implementation
DynamicMemoryAllocator::DynamicMemoryAllocator() {}
DynamicMemoryAllocator::~DynamicMemoryAllocator() {}
void* DynamicMemoryAllocator::Allocate_Bytes_No_Zero(int bytes) { return malloc(bytes); }
void DynamicMemoryAllocator::Free_Bytes(void *block) { free(block); }
int DynamicMemoryAllocator::Get_Actual_Allocation_Size(int bytes) { return bytes; }
void DynamicMemoryAllocator::Reset() {}

// FastCriticalSectionClass (Minimal)
void FastCriticalSectionClass::Thread_Safe_Set_Flag() {}
void FastCriticalSectionClass::Thread_Safe_Clear_Flag() {}

// Implementations of non-virtual base methods
void FileSystem::Init() {}
void FileSystem::Reset() {}
void FileSystem::Update() {}

File* FileSystem::Open_File(const char* filename, int mode) {
    if (g_theLocalFileSystem) return g_theLocalFileSystem->Open_File(filename, mode);
    return nullptr;
}
bool FileSystem::Does_File_Exist(const char* filename) const {
    if (g_theLocalFileSystem) return g_theLocalFileSystem->Does_File_Exist(filename);
    return false;
}
void FileSystem::Get_File_List_In_Directory(const Utf8String &dir, const Utf8String &filter, std::set<Utf8String, rts::less_than_nocase<Utf8String>> &filelist, bool a5) const {}
bool FileSystem::Get_File_Info(const Utf8String &filename, FileInfo *info) const { return false; }
bool FileSystem::Create_Directory(Utf8String name) { return false; }
bool FileSystem::Are_Music_Files_On_CD() { return false; }
void FileSystem::Load_Music_Files_From_CD() {}
void FileSystem::Unload_Music_Files_From_CD() {}

// SubsystemInterface methods
float SubsystemInterface::s_totalSubsystemTime = 0.0f;
SubsystemInterface::SubsystemInterface() {}
SubsystemInterface::~SubsystemInterface() {}
void SubsystemInterface::Init() {}
void SubsystemInterface::Reset() {}
void SubsystemInterface::Update() {}
void SubsystemInterface::Set_Name(Utf8String name) { m_subsystemName = name; }

// SubsystemInterfaceList methods
void SubsystemInterfaceList::Init_Subsystem(SubsystemInterface *sys, const char *, const char *, const char *, Xfer *, Utf8String sys_name) {
    if (sys) sys->Set_Name(sys_name);
}
void SubsystemInterfaceList::Post_Process_Load_All() {}
void SubsystemInterfaceList::Reset_All() {}
void SubsystemInterfaceList::Shutdown_All() {}
void SubsystemInterfaceList::Add_Subsystem(SubsystemInterface *) {}
void SubsystemInterfaceList::Remove_Subsystem(SubsystemInterface *) {}

// Global helper
void Init_Subsystem(SubsystemInterface *&ptr, const char *name, SubsystemInterface *inst) { ptr = inst; }

namespace Thyme {
    int Encode_Buffered_File_Mode(int mode, int buffer_size) { return mode; }
    bool Name_To_Language(const char *name, LanguageID &lang) { return false; }
    const char *Get_Language_Name(LanguageID language) { return "Unknown"; }
}

// File base methods
File::~File() {}
bool File::Open(const char *f, int m) { return false; }
void File::Close() {}
bool File::Eof() { return false; }
int File::Size() { return 0; }
int File::Position() { return 0; }
bool File::Print(const char *f, ...) { return false; }

// Local File Implementation (Minimal)
class MockFile : public File {
    FILE* m_file;
public:
    void* operator new(size_t size) { return malloc(size); }
    void operator delete(void* ptr) { free(ptr); }

    MockFile(const char* name, int mode) {
        m_name = name;
        m_access = mode;
        const char* fmode = (mode & (WRITE | CREATE | TRUNCATE)) ? "wb" : "rb";
        m_file = fopen(name, fmode);
        m_open = (m_file != nullptr);
    }
    ~MockFile() override { if (m_file) fclose(m_file); }
    int Read(void* buf, int n) override { return m_file ? (int)fread(buf, 1, n, m_file) : 0; }
    int Write(const void* buf, int n) override { return m_file ? (int)fwrite(buf, 1, n, m_file) : 0; }
    int Seek(int offset, SeekMode mode) override { 
        if (!m_file) return -1;
        int origin = SEEK_SET;
        if(mode == CURRENT) origin = SEEK_CUR;
        if(mode == END) origin = SEEK_END;
        return fseek(m_file, offset, origin); 
    }
    void Next_Line(char *dst, int bytes) override { if(m_file) fgets(dst, bytes, m_file); }
    bool Scan_Int(int &i) override { return m_file && fscanf(m_file, "%d", &i) == 1; }
    bool Scan_Real(float &f) override { return m_file && fscanf(m_file, "%f", &f) == 1; }
    bool Scan_String(Utf8String &s) override { char buf[1024]; if(m_file && fscanf(m_file, "%1023s", buf) == 1) { s = buf; return true; } return false; }
    void *Read_Entire_And_Close() override { return nullptr; }
    File *Convert_To_RAM_File() override { return nullptr; }
};

// Hooking into Win32 classes
File *Win32LocalFileSystem::Open_File(const char *filename, int mode) { return new MockFile(filename, mode); }
bool Win32LocalFileSystem::Does_File_Exist(const char *filename) const { FILE* f = fopen(filename, "rb"); if(f) { fclose(f); return true; } return false; }
void Win32LocalFileSystem::Get_File_List_In_Directory(Utf8String const &subdir, Utf8String const &dirpath, Utf8String const &filter, std::set<Utf8String, rts::less_than_nocase<Utf8String>> &filelist, bool search_subdirs) const {}
bool Win32LocalFileSystem::Get_File_Info(Utf8String const &filename, FileInfo *info) const { return false; }
bool Win32LocalFileSystem::Create_Directory(Utf8String dir_path) { return false; }

void Win32BIGFileSystem::Init() {}
ArchiveFile *Win32BIGFileSystem::Open_Archive_File(const char *filename) { return nullptr; }
void Win32BIGFileSystem::Close_Archive_File(const char *filename) {}
bool Win32BIGFileSystem::Load_Big_Files_From_Directory(Utf8String dir, Utf8String filter, bool overwrite) { return false; }

// Dummy ArchiveFileSystem methods
ArchiveFileSystem::~ArchiveFileSystem() {}
File *ArchiveFileSystem::Open_File(const char *filename, int mode) { return nullptr; }
bool ArchiveFileSystem::Does_File_Exist(const char *filename) const { return false; }
void ArchiveFileSystem::Load_Into_Directory_Tree(ArchiveFile const *file, Utf8String const &dir, bool overwrite) {}
`), 0644)

	minimalCmake := fmt.Sprintf(`
cmake_minimum_required(VERSION 3.10)
project(GameTextCompilerMinimal)
set(THYME_ROOT "%[1]s")
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_C_STANDARD 11)

add_definitions(-DSTANDALONE -DBUILD_TOOLS -DPLATFORM_WINDOWS -D_CRT_SECURE_NO_WARNINGS -DUNICODE -D_UNICODE -D__CURRENT_FUNCTION__=__FUNCSIG__ -DNOMINMAX)

include_directories(
    "./src"
    "."
    "${THYME_ROOT}/src"
    "${THYME_ROOT}/src/game"
    "${THYME_ROOT}/src/game/common"
    "${THYME_ROOT}/src/game/common/ini"
    "${THYME_ROOT}/src/game/common/system"
    "${THYME_ROOT}/src/game/common/utility"
    "${THYME_ROOT}/src/game/client"
    "${THYME_ROOT}/src/platform"
    "${THYME_ROOT}/src/tools/gametextcompiler/src"
    "${THYME_ROOT}/src/w3d/lib"
    "${THYME_ROOT}/src/w3d/math"
    "${THYME_ROOT}/deps/baseconfig/src"
    "${THYME_ROOT}/deps/baseconfig/src/strings"
    "${THYME_ROOT}/deps/captnlog/src"
)

add_executable(gametextcompiler
    # Tool source
    "${THYME_ROOT}/src/tools/gametextcompiler/src/main.cpp"
    "${THYME_ROOT}/src/tools/gametextcompiler/src/processor.cpp"
    "${THYME_ROOT}/src/tools/gametextcompiler/src/commands.cpp"
    "${THYME_ROOT}/src/tools/gametextcompiler/src/log.cpp"

    # Engine Core (Minimal)
    "${THYME_ROOT}/src/game/client/gametextfile.cpp"
    "${THYME_ROOT}/src/game/common/system/asciistring.cpp"
    "${THYME_ROOT}/src/game/common/system/unicodestring.cpp"
    "${THYME_ROOT}/src/w3d/lib/wwstring.cpp"
    
    # Base dependencies
    "${THYME_ROOT}/deps/baseconfig/src/stringex.c"
    "${THYME_ROOT}/deps/baseconfig/src/win32compat.c"
    "src/gitverinfo.c"
    
    # Captnlog
    "${THYME_ROOT}/deps/captnlog/src/captnlog.c"
    "${THYME_ROOT}/deps/captnlog/src/captnassert.c"
    "${THYME_ROOT}/deps/captnlog/src/captnmessage_win32.c"
    
    # Mocks
    "src/mocks.cpp"
)

target_link_libraries(gametextcompiler PRIVATE ws2_32 advapi32)
`, filepath.ToSlash(thymeDir))
	os.WriteFile(filepath.Join(tempBuildDir, "CMakeLists.txt"), []byte(minimalCmake), 0644)

	fmt.Println(" [..] Configuring Minimal CMake...")
	os.RemoveAll(filepath.Join(tempBuildDir, "build"))
	cmd := exec.Command("cmake", "-S", ".", "-B", "build", "-DCMAKE_BUILD_TYPE=Release")
	cmd.Dir = tempBuildDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmake config failed: %v\n%s", err, string(output))
	}

	fmt.Println(" [..] Compiling gametextcompiler (Minimal Build)...")
	cmd = exec.Command("cmake", "--build", "build", "--config", "Release")
	cmd.Dir = tempBuildDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmake build failed: %v\n%s", err, string(output))
	}

	exePath := filepath.Join(tempBuildDir, "build", "Release", "gametextcompiler.exe")
	if _, err := os.Stat(exePath); err != nil {
		exePath = filepath.Join(tempBuildDir, "build", "gametextcompiler.exe")
		if _, err := os.Stat(exePath); err != nil {
			return fmt.Errorf("could not find built gametextcompiler.exe")
		}
	}

	data, err := os.ReadFile(exePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(binDir, "gametextcompiler.exe"), data, 0755); err != nil {
		return err
	}

	fmt.Println(" [OK] Cleaning up build artifacts...")
	os.RemoveAll(thymeDir)
	os.RemoveAll(filepath.Dir(tempBuildDir)) // Removes .tools
	return nil
}

func ensureIcons(binDir string) error {
	targetIco := filepath.Join(binDir, "icon.ico")
	targetPng := filepath.Join(binDir, "icon.png")

	if _, err := os.Stat(targetIco); err == nil {
		return nil // Already present
	}

	fmt.Println(" [..] Downloading original icon from GitHub...")
	const iconURL = "https://raw.githubusercontent.com/TheSuperHackers/GeneralsModBuilder/main/ModBuilder/generalsmodbuilder/gui/icon.png"
	
	resp, err := http.Get(iconURL)
	if err != nil {
		return fmt.Errorf("failed to download icon: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download icon: status %d", resp.StatusCode)
	}

	pngData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Save PNG to bin
	os.WriteFile(targetPng, pngData, 0644)

	// Convert to ICO and save to bin
	out, err := os.Create(targetIco)
	if err != nil {
		return err
	}
	defer out.Close()

	binary.Write(out, binary.LittleEndian, uint16(0))
	binary.Write(out, binary.LittleEndian, uint16(1))
	binary.Write(out, binary.LittleEndian, uint16(1))
	binary.Write(out, binary.LittleEndian, uint8(0))
	binary.Write(out, binary.LittleEndian, uint8(0))
	binary.Write(out, binary.LittleEndian, uint8(0))
	binary.Write(out, binary.LittleEndian, uint8(0))
	binary.Write(out, binary.LittleEndian, uint16(1))
	binary.Write(out, binary.LittleEndian, uint16(32))
	binary.Write(out, binary.LittleEndian, uint32(len(pngData)))
	binary.Write(out, binary.LittleEndian, uint32(22))
	out.Write(pngData)
	out.Close() // Explicit close for Windows delete safety

	fmt.Println(" [OK] Icons downloaded and prepared in internal/bin.")
	return nil
}
