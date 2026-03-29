package internal

import (
	"crypto"
	_ "crypto/md5"
	_ "crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"GoModBuilder/internal/changelog"

	"golang.org/x/sys/windows/registry"
)

type ModBuilder struct {
	ItemsConfig   *ModBundleItems
	PacksConfig   *ModBundlePacks
	ProjectDir    string
	CustomGameDir string // Manual Path Override
	BuildDir      string
	ReleaseDir    string
	Folders       *ModFolders
	Logger        func(string)
	LogMutex      sync.Mutex
	Parallel      bool
	procSem       chan struct{} // Global semaphore for external processes

	// Baseline Management
	BaselineFilenames map[string]bool
}

func NewModBuilder(items *ModBundleItems, packs *ModBundlePacks, projectDir string) *ModBuilder {
	b := &ModBuilder{
		ItemsConfig:       items,
		PacksConfig:       packs,
		ProjectDir:        projectDir,
		BuildDir:          filepath.Join(projectDir, "_absBuildDir"),
		ReleaseDir:        filepath.Join(projectDir, "_absReleaseDir"),
		procSem:           make(chan struct{}, runtime.NumCPU()),
		BaselineFilenames: make(map[string]bool),
	}
	b.LoadBaseline()
	return b
}

func (b *ModBuilder) SetProjectDir(dir string) {
	b.ProjectDir = dir
	b.BuildDir = filepath.Join(dir, "_absBuildDir")
	b.ReleaseDir = filepath.Join(dir, "_absReleaseDir")

	// Re-apply custom folder configurations if they exist
	if b.Folders != nil {
		b.SetFolders(b.Folders)
	}
	b.LoadBaseline()
}

func (b *ModBuilder) LoadBaseline() {
	baselinePath := filepath.Join(b.ProjectDir, "VanillaBaseline.json")
	data, err := os.ReadFile(baselinePath)
	if err == nil {
		json.Unmarshal(data, &b.BaselineFilenames)
	}
}

func (b *ModBuilder) SaveBaseline() error {
	baselinePath := filepath.Join(b.ProjectDir, "VanillaBaseline.json")
	data, _ := json.MarshalIndent(b.BaselineFilenames, "", "  ")
	return os.WriteFile(baselinePath, data, 0644)
}

func (b *ModBuilder) RefreshBaseline(targetGameDir string) error {
	b.log("Taking recursive snapshot of ALL game files for Vanilla Baseline: %s", targetGameDir)
	b.BaselineFilenames = make(map[string]bool)

	err := filepath.Walk(targetGameDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return filepath.SkipDir // If we can't access it, just skip it!
		}

		// Skip hidden/system folders (like $Recycle.Bin or .git)
		if info.IsDir() && (strings.HasPrefix(info.Name(), "$") || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(targetGameDir, path)
		normalized := strings.ToLower(filepath.ToSlash(rel))

		// If it's a known project file, DON'T include it in the vanilla baseline!
		if b.isProjectFile(normalized) {
			return nil
		}

		// Also skip the tool's own metadata files to avoid feedback loops
		if normalized == "installedthings.json" || normalized == "vanillabaseline.json" ||
			normalized == "projectfolders.json" || strings.HasSuffix(normalized, ".log") {
			return nil
		}

		b.BaselineFilenames[normalized] = true
		return nil
	})

	if err != nil {
		return err
	}

	b.log("  Recursive Snapshot complete: %d items recorded as Vanilla.", len(b.BaselineFilenames))
	return b.SaveBaseline()
}

func (b *ModBuilder) SetFolders(f *ModFolders) {
	b.Folders = f
	if f.Folders.BuildDir != "" {
		if filepath.IsAbs(f.Folders.BuildDir) {
			b.BuildDir = f.Folders.BuildDir
		} else {
			b.BuildDir = filepath.Join(b.ProjectDir, f.Folders.BuildDir)
		}
	}
	if f.Folders.ReleaseDir != "" {
		if filepath.IsAbs(f.Folders.ReleaseDir) {
			b.ReleaseDir = f.Folders.ReleaseDir
		} else {
			b.ReleaseDir = filepath.Join(b.ProjectDir, f.Folders.ReleaseDir)
		}
	}
}

func (b *ModBuilder) log(format string, a ...interface{}) {
	b.LogMutex.Lock()
	defer b.LogMutex.Unlock()
	msg := fmt.Sprintf(format, a...)
	fmt.Println(msg)
	if b.Logger != nil {
		b.Logger(msg)
	}
}

func (b *ModBuilder) CleanAll() error {
	b.log("Cleaning build and release directories...")
	os.RemoveAll(b.BuildDir)
	os.RemoveAll(b.ReleaseDir)
	os.MkdirAll(b.BuildDir, 0755)
	os.MkdirAll(b.ReleaseDir, 0755)
	return nil
}

func (b *ModBuilder) RunGame(gameDir, exeName, language, launchArgs string) error {
	finalDir := b.GetGameDir(gameDir, exeName)
	fullPath := filepath.Join(finalDir, exeName)

	if language != "" {
		if err := b.SetGameLanguage(exeName, language); err != nil {
			b.log("  Warning: failed to set registry language: %v", err)
		}
	}

	b.log("Launching %s from: %s with args: %s", exeName, finalDir, launchArgs)

	// Parse space-separated arguments
	args := strings.Fields(launchArgs)
	cmd := exec.Command(fullPath, args...)
	cmd.Dir = finalDir
	return cmd.Start()
}

func (b *ModBuilder) SetGameLanguage(exeName, language string) error {
	if language == "" {
		return nil
	}

	b.log("Attempting to set Game Language to '%s' for %s...", language, exeName)

	regPaths := b.GetLanguageRegistryKeys(exeName)

	// Registry access rules
	accesses := []struct {
		Root  registry.Key
		Name  string
		Flags uint32
	}{
		{registry.CURRENT_USER, "HKCU", registry.QUERY_VALUE | registry.SET_VALUE},                         // HKCU standard
		{registry.LOCAL_MACHINE, "HKLM", registry.QUERY_VALUE | registry.SET_VALUE | registry.WOW64_32KEY}, // HKLM 32-bit Redirect
	}

	var lastErr error // To capture the last error if no key is successfully updated
	var successCount int

	for _, acc := range accesses {
		for _, regPath := range regPaths {
			key, err := registry.OpenKey(acc.Root, regPath, acc.Flags)
			if err != nil {
				// Don't log expected errors for paths that don't exist
				lastErr = err // Keep track of the last error
				continue
			}

			// Backup current value if not already backed up
			backupPath := filepath.Join(b.BuildDir, ".GameLanguage.backup")
			if _, err := os.Stat(backupPath); os.IsNotExist(err) {
				currentVal, _, err := key.GetStringValue("Language")
				if err != nil {
					currentVal, _, err = key.GetStringValue("language")
				}
				if err == nil {
					os.WriteFile(backupPath, []byte(currentVal), 0644)
				}
			}

			// Set new values
			setSuccess := false
			if err := key.SetStringValue("Language", language); err == nil {
				setSuccess = true
			}

			if err := key.SetStringValue("language", language); err == nil {
				setSuccess = true
			}
			// Also sync variations for maximum compatibility
			key.SetStringValue("Language_A", language)
			key.SetStringValue("language_a", language)

			key.Close()

			if setSuccess {
				b.log("  Registry Sync Success: %s\\%s", acc.Name, regPath)
				successCount++
			}
		}
	}

	if successCount == 0 && lastErr != nil {
		return fmt.Errorf("failed to update language in registry: %v (Is tool running as Admin?)", lastErr)
	}

	return nil
}

func (b *ModBuilder) RestoreGameLanguage(exeName string) error {
	backupPath := filepath.Join(b.BuildDir, ".GameLanguage.backup")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return nil // No backup, nothing to restore
	}
	defer os.Remove(backupPath)

	lang := string(data)
	b.log("Restoring Game Language to '%s' for %s...", lang, exeName)

	regPaths := b.GetLanguageRegistryKeys(exeName)
	accesses := []struct {
		Root  registry.Key
		Flags uint32
	}{
		{registry.CURRENT_USER, registry.SET_VALUE},
		{registry.LOCAL_MACHINE, registry.SET_VALUE | registry.WOW64_32KEY},
		{registry.CURRENT_USER, registry.SET_VALUE | registry.WOW64_32KEY},
	}

	for _, acc := range accesses {
		for _, regPath := range regPaths {
			key, err := registry.OpenKey(acc.Root, regPath, acc.Flags)
			if err != nil {
				continue
			}
			key.SetStringValue("Language", lang)
			key.SetStringValue("language", lang)
			key.SetStringValue("Language_A", lang)
			key.Close()
		}
	}
	return nil
}

func (b *ModBuilder) GetLanguageRegistryKeys(exeName string) []string {
	lowerExe := strings.ToLower(exeName)
	if strings.Contains(lowerExe, "zh") || strings.Contains(lowerExe, "zerohour") {
		return []string{
			`SOFTWARE\Electronic Arts\EA Games\Command and Conquer Generals Zero Hour`,
			`SOFTWARE\Electronic Arts\EA Games\ZeroHour`,
			`SOFTWARE\Electronic Arts\EA Games\Command and Conquer Generals Zero Hour\1.0`,
			`SOFTWARE\EA Games\Command and Conquer Generals Zero Hour`,
			`SOFTWARE\EA Games\ZeroHour`,
		}
	}
	// Vanilla Generals paths
	return []string{
		`SOFTWARE\Electronic Arts\EA Games\Generals`,
		`SOFTWARE\Electronic Arts\EA Games\Generals\1.0`,
		`SOFTWARE\Electronic Arts\EA Games\Command and Conquer Generals`,
		`SOFTWARE\Electronic Arts\EA Games\Command and Conquer Generals\1.0`,
		`SOFTWARE\EA Games\Generals`,
		`SOFTWARE\EA Games\Command and Conquer Generals`,
	}
}

func (b *ModBuilder) GetGameDir(defaultDir, exeName string) string {
	if b.CustomGameDir != "" {
		return b.CustomGameDir
	}
	dir := defaultDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.ProjectDir, defaultDir)
	}

	if _, err := os.Stat(filepath.Join(dir, exeName)); err == nil {
		return dir
	}

	if smartPath := b.FindGameInstallPath(exeName); smartPath != "" {
		return smartPath
	}

	return dir
}

func (b *ModBuilder) LoadState() (*InstalledState, error) {
	statePath := filepath.Join(b.BuildDir, "InstalledThings.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &InstalledState{Files: []InstalledFile{}}, nil
		}
		return nil, err
	}
	var state InstalledState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (b *ModBuilder) SaveState(state *InstalledState) error {
	statePath := filepath.Join(b.BuildDir, "InstalledThings.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0644)
}

func (b *ModBuilder) FindGameInstallPath(exeName string) string {
	keys := []string{
		`SOFTWARE\WOW6432Node\Electronic Arts\EA Games\Generals`,
		`SOFTWARE\WOW6432Node\Electronic Arts\EA Games\Command and Conquer Generals Zero Hour`,
		`SOFTWARE\WOW6432Node\Electronic Arts\EA Games\Command and Conquer Generals`,
		`SOFTWARE\WOW6432Node\Electronic Arts\Generals`,
		`SOFTWARE\Electronic Arts\EA Games\Generals`,
		`SOFTWARE\Electronic Arts\EA Games\Command and Conquer Generals Zero Hour`,
		`SOFTWARE\Electronic Arts\EA Games\Command and Conquer Generals`,
	}

	for _, k := range keys {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, k, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, err := key.GetStringValue("InstallPath")
		key.Close()
		if err == nil && val != "" {
			if _, err := os.Stat(filepath.Join(val, exeName)); err == nil {
				return val
			}
		}
	}
	return ""
}

func (b *ModBuilder) Uninstall(exeName string) error {
	b.log("Starting uninstall process...")

	state, err := b.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load install state: %v", err)
	}

	if len(state.Files) == 0 {
		b.log("No installed files found to uninstall.")
	}

	for _, file := range state.Files {
		// If there's a backup, restore it
		if file.Backup != "" {
			if _, err := os.Stat(file.Backup); err == nil {
				b.log("  Restoring backup: %s", filepath.Base(file.Target))
				if err := os.Rename(file.Backup, file.Target); err != nil {
					b.log("    Warning: failed to restore backup %s: %v", file.Backup, err)
				}
				continue
			}
		}

		// Otherwise, just delete the mod file
		if _, err := os.Stat(file.Target); err == nil {
			b.log("  Removing mod file: %s", filepath.Base(file.Target))
			if err := os.Remove(file.Target); err != nil {
				b.log("    Warning: failed to remove file %s: %v", file.Target, err)
			}
		}
	}

	// 2. Project-Aware "Deep Cleanup"
	gameDir := b.GetGameDir("", exeName)
	err = filepath.Walk(gameDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(gameDir, path)
		if b.isProjectFile(rel) {
			bakPath := path + ".bak"
			if _, err := os.Stat(bakPath); err == nil {
				b.log("  Restoring orphaned backup: %s", rel)
				os.Remove(path)
				os.Rename(bakPath, path)
			} else {
				b.log("  Cleaning up orphaned project file: %s", rel)
				os.Remove(path)
			}
		}
		return nil
	})

	// Restore language if backup exists (Global Cleanup)
	b.RestoreGameLanguage(exeName)

	// Remove state file
	statePath := filepath.Join(b.BuildDir, "InstalledThings.json")
	os.Remove(statePath)

	b.log("Uninstall completed.")
	return nil
}

func (b *ModBuilder) BuildAll(packFilter string) error {
	b.log("Starting build process...")

	os.MkdirAll(b.BuildDir, 0755)
	os.MkdirAll(b.ReleaseDir, 0755)

	if b.Parallel {
		var wg sync.WaitGroup
		var errOnce sync.Once
		var buildErr error

		for _, pack := range b.PacksConfig.Bundles.Packs {
			if packFilter != "" && pack.Name != packFilter {
				continue
			}
			wg.Add(1)
			go func(p BundlePack) {
				defer wg.Done()
				b.log("Building pack (parallel): %s", p.Name)
				if err := b.BuildPack(p); err != nil {
					errOnce.Do(func() { buildErr = err })
				}
			}(pack)
		}
		wg.Wait()
		return buildErr
	} else {
		for _, pack := range b.PacksConfig.Bundles.Packs {
			if packFilter != "" && pack.Name != packFilter {
				continue
			}
			b.log("Building pack: %s", pack.Name)
			if err := b.BuildPack(pack); err != nil {
				return err
			}
		}
	}

	return nil
}

func (b *ModBuilder) BuildRelease(packFilter string) error {
	b.log("Starting release process...")
	os.MkdirAll(b.ReleaseDir, 0755)

	for _, pack := range b.PacksConfig.Bundles.Packs {
		if packFilter != "" && pack.Name != packFilter {
			continue
		}
		b.log("Packaging pack for release: %s", pack.Name)
		if err := b.BuildPack(pack); err != nil {
			return err
		}

		tr := NewToolRunner(filepath.Join(b.ProjectDir, "internal", "bin"), b.ProjectDir, func(s string) { b.log("%s", s) })
		tr.Semaphore = b.procSem
		zipPath := filepath.Join(b.ReleaseDir, pack.Name+".zip")

		relZip, _ := filepath.Rel(b.ProjectDir, zipPath)
		os.Remove(zipPath)

		for _, itemName := range pack.ItemNames {
			item := b.findItemByName(itemName)
			ext := ".big"
			if item != nil && item.BigSuffix != "" {
				ext = item.BigSuffix
			}
			bigFile := filepath.Join(b.ReleaseDir, itemName+ext)
			if _, err := os.Stat(bigFile); err == nil {
				b.log("  Adding to archive: %s", filepath.Base(bigFile))
				relBig, _ := filepath.Rel(b.ProjectDir, bigFile)
				if err := tr.Run7z("a", "-tzip", "-mx9", relZip, relBig); err != nil {
					return fmt.Errorf("7z failed for %s: %v", itemName, err)
				}
			}
		}

		if _, err := os.Stat(zipPath); err == nil {
			b.log("  Generating hashes for: %s", filepath.Base(zipPath))
			b.generateHashFiles(zipPath)
		}
	}
	return nil
}

func (b *ModBuilder) BuildFileHashRegistry(inputDirs []string, outputName string) error {
	b.log("Generating File Hash Registry: %s", outputName)
	regDir := filepath.Join(b.ProjectDir, "Project", "Resources", "FileHashRegistry")
	os.MkdirAll(regDir, 0755)

	outputPath := filepath.Join(regDir, outputName+".json")
	return os.WriteFile(outputPath, []byte("{}"), 0644)
}

func (b *ModBuilder) InstallAll(targetGameDir, packFilter, exeName string) error {
	b.log("Starting installation to: %s", targetGameDir)

	// Automatic Cleanup: Uninstall any previous mod state first to avoid conflicts
	b.Uninstall(exeName)

	state, err := b.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load install state: %v", err)
	}

	// Language Filtering logic:
	// If multiple language packs are selected, only install the LAST one
	// to avoid "File Clutter" crashing the game.
	finalPacks := []BundlePack{}
	var lastLangPack *BundlePack

	// First pass: identify the last language pack
	for _, pack := range b.PacksConfig.Bundles.Packs {
		if packFilter != "" && !strings.EqualFold(pack.Name, packFilter) {
			continue
		}
		if pack.SetGameLanguageOnInstall != "" {
			tempPack := pack // avoid loop var issues
			lastLangPack = &tempPack
		}
	}

	// Second pass: filter the installation list
	for _, pack := range b.PacksConfig.Bundles.Packs {
		if packFilter != "" && !strings.EqualFold(pack.Name, packFilter) {
			continue
		}

		if pack.SetGameLanguageOnInstall != "" {
			if lastLangPack != nil && pack.Name == lastLangPack.Name {
				finalPacks = append(finalPacks, pack)
			} else {
				b.log("  Skipping restricted pack (another language prioritized): %s", pack.Name)
			}
		} else {
			finalPacks = append(finalPacks, pack)
		}
	}

	for _, pack := range finalPacks {
		if err := b.InstallPack(pack, targetGameDir, state, exeName); err != nil {
			return err
		}
	}

	if err := b.SaveState(state); err != nil {
		return err
	}

	// Post-install integrity check
	b.CheckGameInstallFiles(targetGameDir, state)

	return nil
}

func (b *ModBuilder) UninstallAll(targetGameDir, packFilter, exeName string) error {
	// Global Uninstall for the whole state is safer and matches OG tool's state-based logic
	return b.Uninstall(exeName)
}

func (b *ModBuilder) InstallPack(pack BundlePack, targetGameDir string, state *InstalledState, exeName string) error {
	b.log("Installing pack: %s", pack.Name)

	// Pre-Installation logic (Hooks)
	for _, itemName := range pack.ItemNames {
		item := b.findItemByName(itemName)
		ext := ".big"
		if item != nil && item.BigSuffix != "" {
			ext = item.BigSuffix
		}

		bigFile := filepath.Join(b.ReleaseDir, itemName+ext)
		if _, err := os.Stat(bigFile); err == nil {
			dst := filepath.Join(targetGameDir, filepath.Base(bigFile))
			if err := b.installFile(bigFile, dst, targetGameDir, state); err != nil {
				return err
			}
		} else {
			// Item might be raw files only
			itemBuildDir := filepath.Join(b.BuildDir, itemName)
			if info, err := os.Stat(itemBuildDir); err == nil && info.IsDir() {
				b.log("  Installing raw folder: %s -> %s", itemName, targetGameDir)
				err := filepath.Walk(itemBuildDir, func(path string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return err
					}
					rel, _ := filepath.Rel(itemBuildDir, path)
					dst := filepath.Join(targetGameDir, rel)
					return b.installFile(path, dst, targetGameDir, state)
				})
				if err != nil {
					return err
				}
			}
		}
	}

	// Handle Language Switch (Discovery Scan)
	targetLang := pack.SetGameLanguageOnInstall
	if targetLang == "" {
		// If the pack doesn't specify a language, check the items it contains.
		for _, itemName := range pack.ItemNames {
			item := b.findItemByName(itemName)
			if item != nil && item.SetGameLanguageOnInstall != "" {
				targetLang = item.SetGameLanguageOnInstall
				break // Found a language item, use it.
			}
		}
	}

	if targetLang != "" {
		if err := b.SetGameLanguage(exeName, targetLang); err != nil {
			b.log("  Warning: failed to set language: %v", err)
		}
	}

	return nil
}

func (b *ModBuilder) CheckGameInstallFiles(targetGameDir string, state *InstalledState) {
	b.log("Running recursive integrity check...")

	filepath.Walk(targetGameDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Use relative path for comparison with baseline
		rel, _ := filepath.Rel(targetGameDir, path)
		normalized := filepath.ToSlash(rel)

		// Skip backups and metadata
		lower := strings.ToLower(normalized)
		if strings.HasSuffix(lower, ".bak") || lower == "installedthings.json" ||
			lower == "vanillabaseline.json" || lower == "projectfolders.json" {
			return nil
		}

		// Check if it's in our installed state
		found := false
		for _, sf := range state.Files {
			// Compare absolute paths for state match
			if strings.EqualFold(filepath.ToSlash(sf.Target), filepath.ToSlash(path)) {
				found = true
				break
			}
		}

		if !found && !b.isVanillaFile(normalized) {
			b.log("  Warning: Unexpected file found in game directory: %s", normalized)
		}
		return nil
	})
}

func (b *ModBuilder) isVanillaFile(relPath string) bool {
	normalized := strings.ToLower(filepath.ToSlash(relPath))

	// 1. Dynamic Baseline Check (Primary)
	// If the file was present during the initial snapshot (at its exact path), it's Vanilla.
	if b.BaselineFilenames[normalized] {
		return true
	}

	// 2. Hardcoded Whitelist (Fallback for common files)
	name := filepath.Base(normalized)
	vanillaFiles := map[string]bool{
		"generals.exe": true, "generalszh.exe": true, "worldbuilder.exe": true, "launcher.exe": true,
		"game.dat": true, "generals.dat": true, "langdata.dat": true, "patchget.dat": true,
		"generals.lcf": true, "installscript.vdf": true, "steam_appid.txt": true,
		"generals.ico": true, "generalszh.ico": true, "launcher.bmp": true, "install_final.bmp": true,
		"binkw32.dll": true, "mss32.dll": true, "p2xdll.dll": true, "patchw32.dll": true, "steam_api.dll": true,
		"debugwindow.dll": true, "particleeditor.dll": true,
	}
	if vanillaFiles[name] {
		return true
	}

	// Specific Vanilla .big files
	vanillaBigs := map[string]bool{
		"audio.big": true, "audiozh.big": true, "audioenglishzh.big": true,
		"generals.big": true, "generalszh.big": true,
		"mapsgenerals.big": true, "mapszh.big": true,
		"patch.big": true, "patchzh.big": true, "patchdata.big": true,
		"patchini.big": true, "patchwindow.big": true,
		"textures.big": true, "textureszh.big": true,
		"w3d.big": true, "w3dzh.big": true, "w3denglishzh.big": true,
		"window.big": true, "windowzh.big": true,
		"music.big": true, "musiczh.big": true,
		"speech.big": true, "speechzh.big": true, "speechenglishzh.big": true,
		"english.big": true, "englishzh.big": true, "german.big": true,
		"french.big": true, "italian.big": true, "spanish.big": true,
		"korean.big": true, "polish.big": true, "russian.big": true,
		"chinese.big": true, "inizh.big": true, "shaderszh.big": true,
		"terrainzh.big": true, "genseczh.big": true,
	}
	if vanillaBigs[name] {
		return true
	}

	if strings.HasSuffix(name, ".doc") || strings.HasSuffix(name, ".txt") {
		return true
	}

	// Steam numerical files (like 00000000.016)
	if matched, _ := regexp.MatchString(`^[0-9]+\.[0-9]+$`, name); matched {
		return true
	}

	return false
}

func (b *ModBuilder) isProjectFile(relPath string) bool {
	normalized := strings.ToLower(filepath.ToSlash(relPath))
	name := filepath.Base(normalized)

	// Check against all items in the project
	for _, item := range b.ItemsConfig.Bundles.Items {
		// BIG files are usually in the root, but we check filename parity
		if strings.EqualFold(item.Name+".big", name) {
			return true
		}
		if item.BigSuffix != "" && strings.EqualFold(item.Name+item.BigSuffix, name) {
			return true
		}
	}

	// Pattern matches for project naming convention
	if strings.HasPrefix(name, "core") && strings.HasSuffix(name, ".big") {
		return true
	}

	return false
}

func (b *ModBuilder) installFile(src, dst, targetGameDir string, state *InstalledState) error {
	rel, _ := filepath.Rel(targetGameDir, dst)
	name := filepath.Base(dst)
	useSymlink := false // Could be a config flag
	var finalBackupPath string

	// Check for existing file
	if _, err := os.Stat(dst); err == nil {
		// 1. If it's a known project file, just delete it.
		if b.isProjectFile(rel) {
			os.Remove(dst)
		} else if b.isVanillaFile(rel) {
			// 2. ONLY backup if it's a true Vanilla game file.
			bakPath := dst + ".bak"
			if _, err := os.Stat(bakPath); os.IsNotExist(err) {
				b.log("    Backing up vanilla file: %s", rel)
				if err := os.Rename(dst, bakPath); err != nil {
					return fmt.Errorf("failed to backup %s: %v", dst, err)
				}
				finalBackupPath = bakPath
			} else {
				// Already backed up, just remove current to make room
				os.Remove(dst)
				finalBackupPath = bakPath
			}
		} else {
			// 3. Not vanilla and not recognized as mod? Overwrite to be safe.
			os.Remove(dst)
		}
	}

	os.MkdirAll(filepath.Dir(dst), 0755)

	if useSymlink {
		b.log("    Symlinking: %s", name)
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("failed to create symlink %s -> %s: %v", src, dst, err)
		}
	} else {
		b.log("    Installing: %s", name)
		if err := b.copyFile(src, dst); err != nil {
			return err
		}
	}

	// Add to state if not already there
	found := false
	for i, f := range state.Files {
		if f.Target == dst {
			state.Files[i].Backup = finalBackupPath
			found = true
			break
		}
	}
	if !found {
		state.Files = append(state.Files, InstalledFile{
			Target: dst,
			Backup: finalBackupPath,
		})
	}

	return nil
}

func (b *ModBuilder) copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func (b *ModBuilder) BuildPack(pack BundlePack) error {
	var wg sync.WaitGroup
	var errOnce sync.Once
	var buildErr error

	if b.Parallel {
		for _, itemName := range pack.ItemNames {
			item := b.findItemByName(itemName)
			if item == nil {
				return fmt.Errorf("item not found: %s", itemName)
			}

			wg.Add(1)
			go func(it BundleItem) {
				defer wg.Done()

				if err := b.runEvent(it.OnPreBuild, "OnPreBuild"); err != nil {
					errOnce.Do(func() { buildErr = err })
					return
				}

				if err := b.BuildItem(it); err != nil {
					errOnce.Do(func() { buildErr = err })
					return
				}

				if err := b.runEvent(it.OnPostBuild, "OnPostBuild"); err != nil {
					errOnce.Do(func() { buildErr = err })
					return
				}
			}(*item)
		}
		wg.Wait()
	} else {
		for _, itemName := range pack.ItemNames {
			item := b.findItemByName(itemName)
			if item == nil {
				return fmt.Errorf("item not found: %s", itemName)
			}

			if err := b.runEvent(item.OnPreBuild, "OnPreBuild"); err != nil {
				return err
			}
			if err := b.BuildItem(*item); err != nil {
				return err
			}
			if err := b.runEvent(item.OnPostBuild, "OnPostBuild"); err != nil {
				return err
			}
		}
	}
	return buildErr
}

func (b *ModBuilder) BuildItem(item BundleItem) error {
	b.log("  Building item: %s", item.Name)

	itemBuildDir := filepath.Join(b.BuildDir, item.Name)
	os.MkdirAll(itemBuildDir, 0755)

	for _, file := range item.Files {
		if err := b.ProcessFile(item, file, itemBuildDir); err != nil {
			return err
		}
	}

	if item.Big {
		files, err := os.ReadDir(itemBuildDir)
		if err != nil || len(files) == 0 {
			b.log("  Skipping BIG creation for empty item: %s", item.Name)
			return nil
		}

		b.log("  Creating BIG for item: %s", item.Name)
		bigFile := filepath.Join(b.ReleaseDir, item.Name+".big")
		if item.BigSuffix != "" {
			bigFile = filepath.Join(b.ReleaseDir, item.Name+item.BigSuffix)
		}

		tr := NewToolRunner(filepath.Join(b.ProjectDir, "internal", "bin"), b.ProjectDir, func(s string) { b.log("%s", s) })
		tr.Semaphore = b.procSem
		relBig, _ := filepath.Rel(b.ProjectDir, bigFile)
		relSrc, _ := filepath.Rel(b.ProjectDir, itemBuildDir)
		if err := tr.RunBigCreator("-dest", relBig, "-source", relSrc); err != nil {
			return fmt.Errorf("failed to create BIG for %s: %v", item.Name, err)
		}
	}

	return nil
}

func (b *ModBuilder) ProcessFile(item BundleItem, file BundleFile, targetDir string) error {
	sourceBase := filepath.Join(b.ProjectDir, "Project", file.SourceParent)
	if _, err := os.Stat(sourceBase); os.IsNotExist(err) {
		sourceBase = filepath.Join(b.ProjectDir, file.SourceParent)
	}

	if file.Source != "" && file.Target != "" {
		srcPath := filepath.Join(sourceBase, file.Source)
		dstPath := filepath.Join(targetDir, file.Target)
		return b.processInternal(item, file, srcPath, dstPath, file.Params)
	}

	if b.Parallel {
		var wg sync.WaitGroup
		var errOnce sync.Once
		var buildErr error

		for _, st := range file.SourceTargetList {
			matches, _ := b.recursiveGlob(filepath.Join(sourceBase, st.Source))
			for _, match := range matches {
				wg.Add(1)
				go func(m, t string) {
					defer wg.Done()
					dstPath := b.resolveTargetWildcard(sourceBase, m, targetDir, t)
					if err := b.processInternal(item, file, m, dstPath, file.Params); err != nil {
						errOnce.Do(func() { buildErr = err })
					}
				}(match, st.Target)
			}
		}

		for _, pattern := range file.SourceList {
			matches, _ := b.recursiveGlob(filepath.Join(sourceBase, pattern))
			for _, match := range matches {
				wg.Add(1)
				go func(m string) {
					defer wg.Done()
					rel, _ := filepath.Rel(sourceBase, m)
					dstPath := filepath.Join(targetDir, rel)
					if err := b.processInternal(item, file, m, dstPath, file.Params); err != nil {
						errOnce.Do(func() { buildErr = err })
					}
				}(match)
			}
		}
		wg.Wait()
		return buildErr
	} else {
		for _, st := range file.SourceTargetList {
			matches, _ := b.recursiveGlob(filepath.Join(sourceBase, st.Source))
			for _, match := range matches {
				dstPath := b.resolveTargetWildcard(sourceBase, match, targetDir, st.Target)
				if err := b.processInternal(item, file, match, dstPath, file.Params); err != nil {
					return err
				}
			}
		}

		for _, pattern := range file.SourceList {
			matches, _ := b.recursiveGlob(filepath.Join(sourceBase, pattern))
			for _, match := range matches {
				rel, _ := filepath.Rel(sourceBase, match)
				dstPath := filepath.Join(targetDir, rel)
				if err := b.processInternal(item, file, match, dstPath, file.Params); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (b *ModBuilder) recursiveGlob(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		return filepath.Glob(pattern)
	}

	// Simple recursive glob implementation using filepath.Walk
	base := pattern[:strings.Index(pattern, "**")]
	base = filepath.Dir(base)

	// Convert pattern to regex
	rePattern := strings.ReplaceAll(pattern, "\\", "/")
	rePattern = regexp.QuoteMeta(rePattern)
	rePattern = strings.ReplaceAll(rePattern, "\\*\\*/", "(.+/)?")
	rePattern = strings.ReplaceAll(rePattern, "\\*\\*", ".*")
	rePattern = strings.ReplaceAll(rePattern, "\\*", "[^/]+")
	rePattern = "^" + rePattern + "$"

	re, err := regexp.Compile(rePattern)
	if err != nil {
		return nil, err
	}

	var matches []string
	err = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return filepath.SkipDir // If we can't access it, just skip it!
		}

		// Skip hidden/system folders (like $Recycle.Bin or .git)
		if info.IsDir() && (strings.HasPrefix(info.Name(), "$") || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}
		normalizedPath := strings.ReplaceAll(path, "\\", "/")
		if re.MatchString(normalizedPath) {
			matches = append(matches, path)
		}
		return nil
	})

	return matches, err
}

func (b *ModBuilder) resolveTargetWildcard(sourceBase, matchPath, targetBase, targetPattern string) string {
	if !strings.Contains(targetPattern, "**") && !strings.Contains(targetPattern, "*") {
		return filepath.Join(targetBase, targetPattern)
	}

	relMatch, _ := filepath.Rel(sourceBase, matchPath)
	sourceParts := strings.Split(filepath.ToSlash(relMatch), "/")

	targetParts := strings.Split(filepath.ToSlash(targetPattern), "/")
	finalParts := make([]string, 0, len(targetParts))

	for i, part := range targetParts {
		if part == "**" {
			// Take all remaining source parts except the last one (the file name)
			if i < len(sourceParts)-1 {
				finalParts = append(finalParts, sourceParts[i:len(sourceParts)-1]...)
			}
			// After ** we only expect the filename part
			break
		} else if strings.Contains(part, "*") {
			// Simple * substitution for filename or extension
			sourceFile := sourceParts[len(sourceParts)-1]
			sourceName := strings.TrimSuffix(sourceFile, filepath.Ext(sourceFile))
			sourceExt := filepath.Ext(sourceFile)

			targetFilePart := part
			if targetFilePart == "*" || targetFilePart == "*.*" {
				finalParts = append(finalParts, sourceFile)
			} else {
				// Handle things like *.csf or generals.*
				targetName := strings.TrimSuffix(targetFilePart, filepath.Ext(targetFilePart))
				targetExt := filepath.Ext(targetFilePart)

				newName := sourceName
				if targetName != "*" {
					newName = targetName
				}
				newExt := sourceExt
				if targetExt != ".*" && targetExt != "" {
					newExt = targetExt
				}
				finalParts = append(finalParts, newName+newExt)
			}
			break
		} else if i < len(sourceParts)-1 {
			// Ordinary path part
			finalParts = append(finalParts, part)
		}
	}

	return filepath.Join(targetBase, filepath.Join(finalParts...))
}

func (b *ModBuilder) processInternal(item BundleItem, file BundleFile, srcPath, dstPath string, params map[string]interface{}) error {
	if strings.HasSuffix(strings.ToLower(srcPath), ".str") {
		if !strings.HasSuffix(strings.ToLower(dstPath), ".csf") {
			dstPath = strings.TrimSuffix(dstPath, filepath.Ext(dstPath)) + ".csf"
		}
		return b.compileGameText(item, file, srcPath, dstPath, params)
	}

	if strings.HasSuffix(strings.ToLower(srcPath), ".csf") && strings.HasSuffix(strings.ToLower(dstPath), ".str") {
		return b.decompileGameText(item, file, srcPath, dstPath, params)
	}

	// Texture optimization
	srcExt := strings.ToLower(filepath.Ext(srcPath))
	dstExt := strings.ToLower(filepath.Ext(dstPath))
	if (srcExt == ".psd" || srcExt == ".tif" || srcExt == ".tga" || srcExt == ".dds") && (dstExt == ".dds" || dstExt == ".tga") {
		return b.processTexture(item, file, srcPath, dstPath, params)
	}

	return b.copyAndTransform(item, file, srcPath, dstPath, params)
}

func (b *ModBuilder) processTexture(item BundleItem, file BundleFile, srcPath, dstPath string, params map[string]interface{}) error {
	dstExt := strings.ToLower(filepath.Ext(dstPath))

	if dstExt == ".dds" {
		b.log("    Optimizing texture: %s -> %s", filepath.Base(srcPath), filepath.Base(dstPath))
		os.MkdirAll(filepath.Dir(dstPath), 0755)

		tr := NewToolRunner(filepath.Join(b.ProjectDir, "internal", "bin"), b.ProjectDir, func(s string) { b.log("%s", s) })
		tr.Semaphore = b.procSem

		// Basic crunch arguments
		relSrc, _ := filepath.Rel(b.ProjectDir, srcPath)
		relDst, _ := filepath.Rel(b.ProjectDir, dstPath)

		args := []string{"-file", relSrc, "-out", relDst, "-fileformat", "dds", "-noprogress"}

		// TODO: Auto-select DXT1/DXT5 based on alpha if possible.
		// For now, use DXT5 to be safe, or allow user params.
		hasFormat := false
		for k := range params {
			if strings.HasPrefix(k, "-") {
				args = append(args, k)
				if val, ok := params[k].(string); ok && val != "" {
					args = append(args, val)
				} else if val, ok := params[k].(int); ok {
					args = append(args, fmt.Sprintf("%d", val))
				} else if val, ok := params[k].(float64); ok {
					args = append(args, fmt.Sprintf("%g", val))
				}
				if strings.EqualFold(k, "-DXT1") || strings.EqualFold(k, "-DXT5") {
					hasFormat = true
				}
			}
		}

		if !hasFormat {
			args = append(args, "-DXT5")
		}

		if err := tr.RunCrunch(args...); err != nil {
			// Fallback to simple copy if crunch fails (e.g. source is PSD/TIFF which crunch doesn't support directly)
			b.log("      Crunch failed or unsupported source: %v. Copying raw if compatible.", err)
			if srcExt := strings.ToLower(filepath.Ext(srcPath)); srcExt == ".tga" || srcExt == ".dds" {
				return b.copyAndTransform(item, file, srcPath, dstPath, params)
			}
			return fmt.Errorf("failed to process texture %s: %v", srcPath, err)
		}
		return nil
	}

	// If target is TGA but source is PSD/TIFF, we'd need conversion.
	// For now, just copy and hope for the best (or log warning).
	return b.copyAndTransform(item, file, srcPath, dstPath, params)
}

func (b *ModBuilder) compileGameText(item BundleItem, file BundleFile, srcPath, dstPath string, params map[string]interface{}) error {
	b.log("    Compiling CSF: %s -> %s", filepath.Base(srcPath), filepath.Base(dstPath))

	// Skip empty .str files as gametextcompiler cannot handle them
	if info, err := os.Stat(srcPath); err == nil && info.Size() == 0 {
		b.log("      Skipping empty STR file: %s", filepath.Base(srcPath))
		return nil
	}

	os.MkdirAll(filepath.Dir(dstPath), 0755)

	tmpStr := dstPath + ".tmp_str"
	if err := b.copyAndTransform(item, file, srcPath, tmpStr, params); err != nil {
		return fmt.Errorf("failed to clean STR file %s: %v", srcPath, err)
	}
	defer os.Remove(tmpStr)

	// Skip if the cleaned STR file is effectively empty (e.g. contained only comments/markers/whitespace)
	if cleanedData, err := os.ReadFile(tmpStr); err == nil {
		contentStr := string(cleanedData)
		lines := strings.Split(contentStr, "\n")
		hasValidContent := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Ignore empty lines and common comment styles
			if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// A valid .str entry usually contains a quote
			if strings.Contains(trimmed, "\"") {
				hasValidContent = true
				break
			}
		}

		if !hasValidContent {
			b.log("      Skipping STR file with no valid string definitions: %s", filepath.Base(srcPath))
			return nil
		}
	}

	tr := NewToolRunner(filepath.Join(b.ProjectDir, "internal", "bin"), b.ProjectDir, func(s string) { b.log("%s", s) })
	tr.Semaphore = b.procSem
	relSrc, _ := filepath.Rel(b.ProjectDir, tmpStr)
	relDst, _ := filepath.Rel(b.ProjectDir, dstPath)

	args := []string{}

	// Handle flags according to Python ModBuilder logic
	if lang, ok := params["language"].(string); ok && lang != "" {
		args = append(args, "-LOAD_STR_LANGUAGES", lang)
		args = append(args, "-LOAD_STR", relSrc)
	} else if swap, ok := params["swapAndSetLanguage"].(string); ok && swap != "" {
		args = append(args, "-LOAD_STR", relSrc)
		args = append(args, "-SWAP_AND_SET_LANGUAGE", swap)
	} else {
		args = append(args, "-LOAD_STR", relSrc)
	}

	args = append(args, "-SAVE_CSF", relDst)

	if err := tr.RunGameTextCompiler(args...); err != nil {
		return fmt.Errorf("failed to compile CSF %s: %v", srcPath, err)
	}
	return nil
}

func (b *ModBuilder) decompileGameText(_ BundleItem, _ BundleFile, srcPath, dstPath string, _ map[string]interface{}) error {
	b.log("    Decompiling CSF: %s -> %s", filepath.Base(srcPath), filepath.Base(dstPath))
	os.MkdirAll(filepath.Dir(dstPath), 0755)

	tr := NewToolRunner(filepath.Join(b.ProjectDir, "internal", "bin"), b.ProjectDir, func(s string) { b.log("%s", s) })
	tr.Semaphore = b.procSem
	relSrc, _ := filepath.Rel(b.ProjectDir, srcPath)
	relDst, _ := filepath.Rel(b.ProjectDir, dstPath)

	args := []string{"-LOAD_CSF", relSrc, "-SAVE_STR", relDst}

	if err := tr.RunGameTextCompiler(args...); err != nil {
		return fmt.Errorf("failed to decompile CSF %s: %v", srcPath, err)
	}
	return nil
}

func (b *ModBuilder) copyAndTransform(_ BundleItem, file BundleFile, src, dst string, params map[string]interface{}) error {
	info, err := os.Stat(src)
	if err != nil || info.IsDir() {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	content := string(data)

	if forceEOL, ok := params["forceEOL"].(string); ok {
		content = b.applyEOL(content, forceEOL)
	}

	// Handle comments
	if deleteComments, ok := params["deleteComments"].(string); ok && deleteComments != "" {
		content = b.removeComments(content, deleteComments)
	}

	// Handle markers (both from struct and params)
	markers := file.ExcludeMarkersList
	if pMarkers, ok := params["excludeMarkersList"].([]interface{}); ok {
		for _, pm := range pMarkers {
			if pair, ok := pm.([]interface{}); ok && len(pair) >= 2 {
				if s, ok1 := pair[0].(string); ok1 {
					if e, ok2 := pair[1].(string); ok2 {
						markers = append(markers, []string{s, e})
					}
				}
			}
		}
	}

	if len(markers) > 0 {
		content = b.removeMarkers(content, markers)
	}

	if deleteWhitespace, ok := params["deleteWhitespace"].(float64); ok && deleteWhitespace > 0 {
		content = b.removeWhitespace(content)
	}
	if deleteWhitespace, ok := params["deleteWhitespace"].(int); ok && deleteWhitespace > 0 {
		content = b.removeWhitespace(content)
	}

	return os.WriteFile(dst, []byte(content), 0644)
}

func (b *ModBuilder) removeWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}

func (b *ModBuilder) removeMarkers(content string, markers [][]string) string {
	for _, pair := range markers {
		if len(pair) < 2 {
			continue
		}
		start, end := pair[0], pair[1]
		for {
			sIdx := strings.Index(content, start)
			if sIdx == -1 {
				break
			}
			rest := content[sIdx+len(start):]
			eIdx := strings.Index(rest, end)
			if eIdx == -1 {
				break
			}
			content = content[:sIdx] + rest[eIdx+len(end):]
		}
	}
	return content
}

func (b *ModBuilder) runEvent(ev *EventConfig, name string) error {
	if ev == nil || ev.Script == "" {
		return nil
	}
	scriptPath := filepath.Join(b.ProjectDir, ev.Script)
	if _, err := os.Stat(scriptPath); err != nil {
		scriptPath = filepath.Join(b.ProjectDir, "Project", ev.Script)
	}

	b.log("  Running event %s: %s", name, ev.Script)
	cmd := exec.Command("python", scriptPath)
	cmd.Dir = b.ProjectDir
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		b.log("    %s", string(output))
	}
	return err
}

func (b *ModBuilder) applyEOL(content, eol string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.ReplaceAll(content, "\n", eol)
}

func (b *ModBuilder) removeComments(content, marker string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, marker); idx != -1 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

func (b *ModBuilder) MakeChangeLog() error {
	b.log("Generating project change log...")
	configPath := filepath.Join(b.ProjectDir, "Project", "ModChangeLog.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(b.ProjectDir, "ModChangeLog.json")
	}

	cfg, err := LoadChangeLogConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load ModChangeLog.json: %v", err)
	}

	for _, record := range cfg.Changelog.Records {
		clRecord := changelog.ChangelogRecord{
			SourcePatterns: record.SourceList,
			TargetFiles:    record.TargetList,
			IncludeLabels:  record.IncludeLabelList,
			ExcludeLabels:  record.ExcludeLabelList,
		}

		for _, sr := range record.SortList {
			rule := changelog.SortRule{}
			if d, ok := sr["date"].(string); ok {
				rule.IsDate = true
				rule.Order = d
			} else if l, ok := sr["label"].(string); ok {
				rule.Label = l
			}
			clRecord.SortRules = append(clRecord.SortRules, rule)
		}

		clConfig := changelog.ChangelogConfig{Records: []changelog.ChangelogRecord{clRecord}}
		entries, err := clConfig.LoadEntries(b.ProjectDir)
		if err != nil {
			b.log("  Warning: failed to load entries: %v", err)
			continue
		}

		changelog.SortEntries(entries, clRecord.SortRules)

		for _, target := range clRecord.TargetFiles {
			dst := filepath.Join(b.ProjectDir, target)
			if err := changelog.GenerateMarkdown(entries, dst); err != nil {
				return fmt.Errorf("failed to generate changelog %s: %v", target, err)
			}
			b.log("  Generated: %s", target)
		}
	}

	b.log("Change Log generation completed.")
	return nil
}

func (b *ModBuilder) findItemByName(name string) *BundleItem {
	for _, item := range b.ItemsConfig.Bundles.Items {
		if item.Name == name {
			return &item
		}
	}
	return nil
}

func (b *ModBuilder) generateHashFiles(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	md5Hash := crypto.MD5.New()
	sha256Hash := crypto.SHA256.New()
	var size int64

	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			md5Hash.Write(buf[:n])
			sha256Hash.Write(buf[:n])
			size += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	md5Str := hex.EncodeToString(md5Hash.Sum(nil))
	sha256Str := hex.EncodeToString(sha256Hash.Sum(nil))

	os.WriteFile(path+".md5", []byte(md5Str), 0644)
	os.WriteFile(path+".sha256", []byte(sha256Str), 0644)
	os.WriteFile(path+".size", []byte(fmt.Sprintf("%d", size)), 0644)

	return nil
}
