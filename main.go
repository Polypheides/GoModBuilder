package main

//go:generate go run gen.go

import (
	"GoModBuilder/internal"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	cwd, _ := os.Getwd()

	buildFlag := flag.Bool("build", false, "Run the build process")
	installFlag := flag.Bool("install", false, "Run the install process")
	setupFlag := flag.Bool("setup", false, "Download and setup required tools")
	changelogFlag := flag.Bool("changelog", false, "Generate project changelog")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose logging")
	guiFlag := flag.Bool("gui", false, "Launch the native Windows GUI")
	uninstallFlag := flag.Bool("uninstall", false, "Run the uninstall process")
	cleanFlag := flag.Bool("clean", false, "Clean build and release directories")
	releaseFlag := flag.Bool("release", false, "Run the release process (packaging)")
	runFlag := flag.Bool("run", false, "Run the game after installation")
	targetFlag := flag.String("target", "_absInstallDir", "Target directory for installation")
	projectDirFlag := flag.String("project", ".", "Directory of the mod project to build")
	packFlag := flag.String("pack", "", "Specific pack to build/install (default all)")
	exeFlag := flag.String("exe", "generals.exe", "Game executable name (generals.exe or generalszh.exe)")

	flag.Parse()

	if *verboseFlag {
		fmt.Println("Verbose logging enabled")
	}

	projectDir := *projectDirFlag
	if !filepath.IsAbs(projectDir) {
		projectDir = filepath.Join(cwd, projectDir)
	}

	// Detect where ModFolders.json is (root or Project/)
	folders := &internal.ModFolders{}
	foldersPath := filepath.Join(projectDir, "ModFolders.json")
	if _, err := os.Stat(foldersPath); os.IsNotExist(err) {
		foldersPath = filepath.Join(projectDir, "Project", "ModFolders.json")
	}

	if f, err := internal.LoadFoldersConfig(foldersPath); err == nil {
		folders = f
	}

	if *setupFlag {
		fmt.Println("Starting setup process...")
		binDir := filepath.Join(projectDir, "internal", "bin") // Match builder's expected tools path
		tr := internal.NewToolRunner(binDir, projectDir, func(s string) { fmt.Println(s) })
		for name, info := range internal.DefaultTools {
			path := filepath.Join(binDir, name)
			if err := tr.DownloadFileWithVerify(info.URL, path, info.Hash); err != nil {
				fmt.Printf(" Failed to setup %s: %v\n", name, err)
			}
		}
		fmt.Println("Setup completed.")
	}

	// Automated configuration discovery from project root (recursive, skipping build/release folders)
	items, packs, err := internal.DiscoverConfigs(projectDir)
	if err != nil {
		log.Fatalf("Configuration discovery failed: %v", err)
	}
	fmt.Printf("Discovery complete: %d items, %d packs found.\n", len(items.Bundles.Items), len(packs.Bundles.Packs))

	b := internal.NewModBuilder(items, packs, projectDir)
	b.SetFolders(folders)

	if *guiFlag || (!*buildFlag && !*installFlag && !*setupFlag && !*changelogFlag && !*uninstallFlag && !*cleanFlag && !*releaseFlag && !*runFlag) {
		internal.Run(items, packs, b)
		return
	}

	// Execution Order: Clean -> Changelog -> Build -> Release -> Uninstall -> Install -> Run
	if *cleanFlag {
		if err := b.CleanAll(); err != nil {
			log.Fatalf("Clean failed: %v", err)
		}
	}

	if *changelogFlag {
		if err := b.MakeChangeLog(); err != nil {
			log.Fatalf("Change Log failed: %v", err)
		}
	}

	if *buildFlag {
		if err := b.BuildAll(*packFlag); err != nil {
			log.Fatalf("Build failed: %v", err)
		}
	}

	if *releaseFlag {
		if err := b.BuildRelease(*packFlag); err != nil {
			log.Fatalf("Release failed: %v", err)
		}
	}

	if *uninstallFlag {
		if err := b.Uninstall(*exeFlag); err != nil {
			log.Fatalf("Uninstall failed: %v", err)
		}
	}

	if *installFlag {
		targetDir := b.GetGameDir(*targetFlag, *exeFlag)
		if err := b.InstallAll(targetDir, *packFlag, *exeFlag); err != nil {
			log.Fatalf("Install failed: %v", err)
		}

		if *runFlag {
			if err := b.RunGame(targetDir, *exeFlag, "", ""); err != nil {
				log.Fatalf("Run failed: %v", err)
			}
		}
	} else if *runFlag {
		targetDir := b.GetGameDir(*targetFlag, *exeFlag)
		if err := b.RunGame(targetDir, *exeFlag, "", ""); err != nil {
			log.Fatalf("Run failed: %v", err)
		}
	}

	fmt.Println("Process completed successfully.")
}
