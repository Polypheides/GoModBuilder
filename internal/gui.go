package internal

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

type ModBuilderWindow struct {
	*walk.MainWindow
	lbPacks             *walk.ListBox
	teLog               *walk.TextEdit
	cbSequenceChangeLog *walk.CheckBox
	cbSequenceClean     *walk.CheckBox
	cbSequenceBuild     *walk.CheckBox
	cbSequenceRelease   *walk.CheckBox
	cbSequenceInstall   *walk.CheckBox
	cbSequenceSnapshot  *walk.CheckBox
	cbSequenceRun       *walk.CheckBox
	cbSequenceUninstall *walk.CheckBox
	cbOptionVerbose     *walk.CheckBox
	cbOptionParallel    *walk.CheckBox
	builder             *ModBuilder
	items               *ModBundleItems
	packs               *ModBundlePacks
	cmExe               *walk.ComboBox
	leProjectDir        *walk.LineEdit
	leGameDir           *walk.LineEdit
	model               []string
}

func Run(items *ModBundleItems, packs *ModBundlePacks, b *ModBuilder) {
	fmt.Println("Initializing GUI...")
	mw := new(ModBuilderWindow)
	mw.items = items
	mw.packs = packs
	mw.builder = b
	b.Logger = mw.log
	mw.model = make([]string, len(packs.Bundles.Packs))
	for i, p := range packs.Bundles.Packs {
		mw.model[i] = p.Name
	}

	if err := (declarative.MainWindow{
		AssignTo: &mw.MainWindow,
		Title:    "Go Mod Builder v1.0 by Polypheides",
		Icon:     mw.getAppIcon(),
		Size:     declarative.Size{Width: 800, Height: 480},
		Font:     declarative.Font{PointSize: 8},
		Layout:   declarative.VBox{MarginsZero: true},
		Children: []declarative.Widget{
			declarative.Composite{
				Layout: declarative.Grid{Columns: 4, Spacing: 2, Margins: declarative.Margins{Left: 2, Top: 2, Right: 2, Bottom: 2}},
				Children: []declarative.Widget{
					declarative.GroupBox{
						Title:  "Bundle Pack list",
						Layout: declarative.VBox{},
						Children: []declarative.Widget{
							declarative.Label{
								Text: "*Hold Ctrl to select multiple packs",
							},
							declarative.ListBox{
								AssignTo:       &mw.lbPacks,
								Model:          mw.model,
								MultiSelection: true, // Native LBS_EXTENDEDSEL
							},
						},
					},
					declarative.GroupBox{
						Title:  "Sequence execution",
						Layout: declarative.VBox{},
						Children: []declarative.Widget{
							declarative.CheckBox{AssignTo: &mw.cbSequenceSnapshot, Text: "Snapshot", Checked: true, ToolTipText: "Captures a vanilla baseline of the game directory to ensure perfect mod removal."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceChangeLog, Text: "Make Change Log"},
							declarative.CheckBox{AssignTo: &mw.cbSequenceClean, Text: "Clean"},
							declarative.CheckBox{AssignTo: &mw.cbSequenceBuild, Text: "Build", Checked: true},
							declarative.CheckBox{AssignTo: &mw.cbSequenceRelease, Text: "Build Release"},
							declarative.CheckBox{AssignTo: &mw.cbSequenceInstall, Text: "Install", Checked: true},
							declarative.CheckBox{AssignTo: &mw.cbSequenceRun, Text: "Run Game", Checked: true},
							declarative.CheckBox{AssignTo: &mw.cbSequenceUninstall, Text: "Uninstall", Checked: true},
							declarative.PushButton{
								Text: "Execute",
								OnClicked: func() {
									mw.executeSequence()
								},
							},
						},
					},
					declarative.GroupBox{
						Title:  "Single actions",
						Layout: declarative.VBox{},
						Children: []declarative.Widget{
							declarative.PushButton{
								Text:        "Snapshot",
								ToolTipText: "Captures a vanilla baseline of the game directory to ensure perfect mod removal.",
								OnClicked: func() {
									gameDir := mw.builder.GetGameDir("", mw.cmExe.Text())
									if err := mw.builder.RefreshBaseline(gameDir); err != nil {
										mw.log(fmt.Sprintf("Error taking snapshot: %v", err))
									} else {
										mw.log("Vanilla Baseline Snapshot created successfully!")
									}
								},
							},
							declarative.PushButton{Text: "Make Change Log", OnClicked: func() { mw.runMakeChangeLog() }},
							declarative.PushButton{Text: "Clean", OnClicked: func() { mw.runClean() }},
							declarative.PushButton{Text: "Build", OnClicked: func() { mw.runBuild() }},
							declarative.PushButton{Text: "Build Release", OnClicked: func() { mw.runBuildRelease() }},
							declarative.PushButton{Text: "Install", OnClicked: func() { mw.runInstall() }},
							declarative.PushButton{Text: "Run Game", OnClicked: func() { mw.runGame() }},
							declarative.PushButton{Text: "Uninstall", OnClicked: func() { mw.runUninstall() }},
							declarative.PushButton{Text: "Abort"},
						},
					},
					declarative.GroupBox{
						Title:  "Options",
						Layout: declarative.VBox{},
						Children: []declarative.Widget{
							declarative.CheckBox{Text: "Auto Clear Console", Checked: true},
							declarative.CheckBox{AssignTo: &mw.cbOptionVerbose, Text: "Verbose Logging", Checked: true},
							declarative.CheckBox{AssignTo: &mw.cbOptionParallel, Text: "Parallel Build (Multithreaded)", Checked: true},
							declarative.Label{Text: "Game Executable:"},
							declarative.ComboBox{
								AssignTo:     &mw.cmExe,
								Model:        []string{"generalszh.exe", "generalsv.exe"},
								CurrentIndex: 0,
							},
							declarative.Label{Text: "Game Directory:"},
							declarative.Composite{
								Layout: declarative.HBox{MarginsZero: true},
								Children: []declarative.Widget{
									declarative.LineEdit{AssignTo: &mw.leGameDir, ReadOnly: true},
									declarative.PushButton{
										Text:    "...",
										MaxSize: declarative.Size{Width: 30},
										OnClicked: func() {
											mw.selectGameDir()
										},
									},
								},
							},
							declarative.Label{Text: "Project Directory:"},
							declarative.Composite{
								Layout: declarative.HBox{MarginsZero: true},
								Children: []declarative.Widget{
									declarative.LineEdit{AssignTo: &mw.leProjectDir, Text: b.ProjectDir, ReadOnly: true},
									declarative.PushButton{
										Text:    "...",
										MaxSize: declarative.Size{Width: 30},
										OnClicked: func() {
											mw.selectProjectDir()
										},
									},
								},
							},
						},
					},
				},
			},
			declarative.TextEdit{
				AssignTo: &mw.teLog,
				ReadOnly: true,
				VScroll:  true,
				MinSize:  declarative.Size{Height: 50},
			},
		},
	}.Create()); err != nil {
		log.Fatal(err)
	}

	// Pre-populate the Game Directory using auto-discovery on startup
	initialExe := mw.cmExe.Text()
	if initialExe == "" {
		initialExe = "generalszh.exe"
	}
	detectedDir := mw.builder.GetGameDir("", initialExe)
	mw.leGameDir.SetText(detectedDir)
	mw.builder.CustomGameDir = detectedDir

	// Intercept Shift key to block range selection
	// The user specifically wants Ctrl-multi-select BUT NO Shift-multi-select
	mw.lbPacks.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if walk.ModifiersDown()&walk.ModShift != 0 {
			// Get item index from point
			// LB_ITEMFROMPOINT = 0x01A9
			res := win.SendMessage(mw.lbPacks.Handle(), 0x01A9, 0, uintptr(win.MAKELONG(uint16(x), uint16(y))))
			itemIdx := int(win.LOWORD(uint32(res)))
			isOutside := win.HIWORD(uint32(res)) != 0

			if !isOutside && itemIdx >= 0 {
				time.AfterFunc(10*time.Millisecond, func() {
					mw.Synchronize(func() {
						mw.lbPacks.SetSelectedIndexes([]int{itemIdx})
					})
				})
			}
		}
	})

	mw.lbPacks.SelectedIndexesChanged().Attach(func() {
		// Summary or auto-update logic could go here
	})

	mw.Run()
}

func (mw *ModBuilderWindow) selectGameDir() {
	dlg := new(walk.FileDialog)
	dlg.Title = "Select Game Directory"

	if ok, _ := dlg.ShowBrowseFolder(mw); ok {
		exeName := mw.cmExe.Text()
		if exeName == "" {
			exeName = "generalszh.exe"
		}

		// Validation check
		hasExe := false
		if _, err := os.Stat(filepath.Join(dlg.FilePath, exeName)); err == nil {
			hasExe = true
		} else if _, err := os.Stat(filepath.Join(dlg.FilePath, "generals.exe")); err == nil {
			hasExe = true
		} else if _, err := os.Stat(filepath.Join(dlg.FilePath, "generalszh.exe")); err == nil {
			hasExe = true
		}

		if !hasExe {
			walk.MsgBox(mw, "Warning", fmt.Sprintf("Neither '%s' nor standard game executables were found in the selected directory.\nThe path will be applied, but the game might fail to run or install.", exeName), walk.MsgBoxIconWarning)
		}

		mw.leGameDir.SetText(dlg.FilePath)
		mw.builder.CustomGameDir = dlg.FilePath
		mw.log(fmt.Sprintf("Game directory manually overridden to: %s", dlg.FilePath))
	}
}

func (mw *ModBuilderWindow) executeSequence() {
	mw.builder.Parallel = mw.cbOptionParallel.Checked()

	if mw.cbSequenceSnapshot.Checked() {
		// To ensure a truly pristine snapshot, we uninstall the active mod FIRST.
		mw.builder.Uninstall(mw.cmExe.Text())
		gameDir := mw.builder.GetGameDir("", mw.cmExe.Text())
		if err := mw.builder.RefreshBaseline(gameDir); err != nil {
			mw.log(fmt.Sprintf("Snapshot error: %v", err))
		} else {
			mw.log("Vanilla Baseline Snapshot updated (Pristine).")
		}
	}

	if mw.cbSequenceChangeLog.Checked() {
		mw.runMakeChangeLog()
	}
	if mw.cbSequenceClean.Checked() {
		mw.runClean()
	}
	if mw.cbSequenceBuild.Checked() {
		mw.runBuild()
	}
	if mw.cbSequenceRelease.Checked() {
		mw.runBuildRelease()
	}
	if mw.cbSequenceInstall.Checked() {
		mw.runInstall()
	}
	if mw.cbSequenceRun.Checked() {
		mw.runGame()
	}
	if mw.cbSequenceUninstall.Checked() {
		mw.runUninstall()
	}
}

func (mw *ModBuilderWindow) runBuild() {
	mw.builder.Parallel = mw.cbOptionParallel.Checked()
	indices := mw.lbPacks.SelectedIndexes()
	if len(indices) == 0 {
		mw.log("No packs selected.")
		return
	}
	for _, idx := range indices {
		packName := mw.packs.Bundles.Packs[idx].Name
		mw.log(fmt.Sprintf("Building pack: %s", packName))
		if err := mw.builder.BuildAll(packName); err != nil {
			mw.log(fmt.Sprintf("Build failed for %s: %v", packName, err))
		} else {
			mw.log(fmt.Sprintf("Build completed for %s.", packName))
		}
	}
}

func (mw *ModBuilderWindow) runBuildRelease() {
	mw.builder.Parallel = mw.cbOptionParallel.Checked()
	indices := mw.lbPacks.SelectedIndexes()
	if len(indices) == 0 {
		mw.log("No packs selected.")
		return
	}
	for _, idx := range indices {
		packName := mw.packs.Bundles.Packs[idx].Name
		mw.log(fmt.Sprintf("Building release for pack: %s", packName))
		if err := mw.builder.BuildRelease(packName); err != nil {
			mw.log(fmt.Sprintf("Release build failed for %s: %v", packName, err))
		} else {
			mw.log(fmt.Sprintf("Release build completed for %s.", packName))
		}
	}
}

func (mw *ModBuilderWindow) runInstall() {
	exeName := mw.cmExe.Text()
	if exeName == "" {
		exeName = "generals.exe"
	}
	gameDir := mw.builder.GetGameDir("_absInstallDir", exeName)
	indices := mw.lbPacks.SelectedIndexes()
	if len(indices) == 0 {
		mw.log("No packs selected.")
		return
	}

	state, err := mw.builder.LoadState()
	if err != nil {
		mw.log(fmt.Sprintf("Error loading install state: %v", err))
		return
	}

	// Language Filtering Logic:
	// If multiple language packs are selected, we only want to install the LAST one
	// to avoid "File Clutter" crashing the game.
	finalPacksToInstall := []BundlePack{}
	var lastLangPack *BundlePack

	// First, find all selected packs
	allSelectedPacks := []BundlePack{}
	for _, idx := range indices {
		pack := mw.packs.Bundles.Packs[idx]
		allSelectedPacks = append(allSelectedPacks, pack)
	}

	// Identify the last language pack
	for i := len(allSelectedPacks) - 1; i >= 0; i-- {
		if allSelectedPacks[i].SetGameLanguageOnInstall != "" {
			lastLangPack = &allSelectedPacks[i]
			break
		}
	}

	// Filter the final list
	for _, pack := range allSelectedPacks {
		if pack.SetGameLanguageOnInstall != "" {
			if lastLangPack != nil && pack.Name == lastLangPack.Name {
				finalPacksToInstall = append(finalPacksToInstall, pack)
			} else {
				mw.log(fmt.Sprintf("  Skipping restricted pack (another language prioritized): %s", pack.Name))
			}
		} else {
			finalPacksToInstall = append(finalPacksToInstall, pack)
		}
	}

	for _, pack := range finalPacksToInstall {
		mw.log(fmt.Sprintf("Installing pack: %s", pack.Name))
		if err := mw.builder.InstallPack(pack, gameDir, state, exeName); err != nil {
			mw.log(fmt.Sprintf("Error installing pack %s: %v", pack.Name, err))
		}
	}

	if err := mw.builder.SaveState(state); err != nil {
		mw.log(fmt.Sprintf("Error saving install state: %v", err))
	} else {
		mw.builder.CheckGameInstallFiles(gameDir, state)
		mw.log("Install completed.")
	}
}

func (mw *ModBuilderWindow) runUninstall() {
	exeName := mw.cmExe.Text()
	if exeName == "" {
		exeName = "generals.exe"
	}
	if err := mw.builder.Uninstall(exeName); err != nil {
		mw.log(fmt.Sprintf("Error during uninstall: %v", err))
	} else {
		mw.log("Uninstall completed.")
	}
}

func (mw *ModBuilderWindow) runClean() {
	mw.log("Cleaning...")
	if err := mw.builder.CleanAll(); err != nil {
		mw.log(fmt.Sprintf("Clean failed: %v", err))
	} else {
		mw.log("Clean completed successfully.")
	}
}

func (mw *ModBuilderWindow) runGame() {
	exeName := mw.cmExe.Text()
	if exeName == "" {
		exeName = "generalszh.exe"
	}

	mw.log(fmt.Sprintf("Launching %s...", exeName))

	if err := mw.builder.RunGame("_absInstallDir", exeName, ""); err != nil {
		mw.log(fmt.Sprintf("Failed to launch game: %v", err))
	}
}

func (mw *ModBuilderWindow) runMakeChangeLog() {
	mw.log("Running Make Change Log...")
	if err := mw.builder.MakeChangeLog(); err != nil {
		mw.log(fmt.Sprintf("Make Change Log failed: %v", err))
	}
}

func (mw *ModBuilderWindow) log(msg string) {
	mw.Synchronize(func() {
		mw.teLog.AppendText(msg + "\r\n")
	})
}

func (mw *ModBuilderWindow) selectProjectDir() {
	dlg := new(walk.FileDialog)
	dlg.Title = "Select Mod Project Directory"

	if ok, _ := dlg.ShowBrowseFolder(mw); ok {
		mw.leProjectDir.SetText(dlg.FilePath)
		mw.builder.ProjectDir = dlg.FilePath
		mw.reDiscover()
	}
}

func (mw *ModBuilderWindow) reDiscover() {
	configDir := mw.builder.ProjectDir
	if _, err := os.Stat(filepath.Join(configDir, "Project")); err == nil {
		configDir = filepath.Join(configDir, "Project")
	}

	items, packs, err := DiscoverConfigs(configDir)
	if err != nil {
		mw.log(fmt.Sprintf("Discovery failed: %v", err))
		return
	}

	mw.items = items
	mw.packs = packs
	mw.builder.ItemsConfig = items
	mw.builder.PacksConfig = packs

	mw.model = make([]string, len(packs.Bundles.Packs))
	for i, p := range packs.Bundles.Packs {
		mw.model[i] = p.Name
	}
	mw.lbPacks.SetModel(mw.model)
	mw.lbPacks.SetSelectedIndexes([]int{})

	mw.log(fmt.Sprintf("Discovery complete: %d items, %d packs found in %s", len(items.Bundles.Items), len(packs.Bundles.Packs), configDir))
}

func (mw *ModBuilderWindow) getAppIcon() interface{} {
	// 1. Try to load from embedded PNG (Highest reliability & easiest fix)
	if len(IconPNG) > 0 {
		img, _, err := image.Decode(bytes.NewReader(IconPNG))
		if err == nil {
			if icon, err := walk.NewIconFromImage(img); err == nil {
				return icon
			}
		}
	}

	// 2. Try local file in internal/bin/icon.ico (Dev or setup)
	iconFile := filepath.Join("internal", "bin", "icon.ico")
	if _, err := os.Stat(iconFile); err == nil {
		return iconFile
	}

	// 3. Try embedded resource IDs (Standard for rsrc icons as fallback)
	for _, id := range []int{7, 3, 1} {
		if icon, err := walk.NewIconFromResourceId(id); err == nil {
			return icon
		}
	}

	return nil
}
