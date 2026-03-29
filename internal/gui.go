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
	cbOptionAutoClear   *walk.CheckBox
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
		Title:    "Go Mod Builder v1.1 by Polypheides",
		Icon:     mw.getAppIcon(),
		Size:     declarative.Size{Width: 950, Height: 550},
		MinSize:  declarative.Size{Width: 850, Height: 450},
		Font:     declarative.Font{Family: "Segoe UI", PointSize: 9}, // Modern UI Font
		Layout:   declarative.VBox{Margins: declarative.Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 10},
		Children: []declarative.Widget{
			declarative.Composite{
				Layout: declarative.HBox{MarginsZero: true, Spacing: 10},
				Children: []declarative.Widget{

					// --- Column 1: Bundle Pack List ---
					declarative.GroupBox{
						Title:         "Bundle Pack list",
						Layout:        declarative.VBox{Spacing: 5},
						StretchFactor: 2, // Expands to take extra width
						Children: []declarative.Widget{
							declarative.Label{
								Text:        "*Hold Ctrl to select multiple packs",
								ToolTipText: "Select one or more packs to process.",
							},
							declarative.ListBox{
								AssignTo:       &mw.lbPacks,
								Model:          mw.model,
								MultiSelection: true, // Native LBS_EXTENDEDSEL
								ToolTipText:    "List of available mod bundle packs.",
							},
						},
					},

					// --- Column 2: Sequence Execution ---
					declarative.GroupBox{
						Title:   "Sequence execution",
						Layout:  declarative.VBox{Spacing: 5},
						MinSize: declarative.Size{Width: 150}, // Increased for better padding
						MaxSize: declarative.Size{Width: 150},
						Children: []declarative.Widget{
							declarative.CheckBox{AssignTo: &mw.cbSequenceSnapshot, Text: "Snapshot", Checked: true, ToolTipText: "Captures a vanilla baseline of the game directory."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceChangeLog, Text: "Changelog", ToolTipText: "Generates project changelog before building."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceClean, Text: "Clean", ToolTipText: "Cleans build and release directories."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceBuild, Text: "Build", Checked: true, ToolTipText: "Builds selected mod packs."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceRelease, Text: "Build Release", ToolTipText: "Packages the built files into release archives."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceInstall, Text: "Install", Checked: true, ToolTipText: "Installs the selected packs into the game directory."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceRun, Text: "Run Game", Checked: true, ToolTipText: "Launches the game after installation."},
							declarative.CheckBox{AssignTo: &mw.cbSequenceUninstall, Text: "Uninstall", Checked: true, ToolTipText: "Uninstalls the current mod before new actions."},
							declarative.VSpacer{}, // Pushes execute button to the bottom
							declarative.PushButton{
								Text:        "Execute",
								Font:        declarative.Font{Bold: true},
								ToolTipText: "Run the selected sequence of actions.",
								OnClicked:   mw.executeSequence,
							},
						},
					},

					// --- Column 3: Single Actions ---
					declarative.GroupBox{
						Title:   "Single actions",
						Layout:  declarative.VBox{Spacing: 5},
						MinSize: declarative.Size{Width: 150}, // Increased for better padding
						MaxSize: declarative.Size{Width: 150},
						Children: []declarative.Widget{
							declarative.PushButton{Text: "Snapshot", ToolTipText: "Captures a vanilla baseline of the game directory.", OnClicked: func() { mw.checkAutoClear(); mw.runSnapshot() }},
							declarative.PushButton{Text: "Changelog", ToolTipText: "Generates the project changelog.", OnClicked: func() { mw.checkAutoClear(); mw.runMakeChangeLog() }},
							declarative.PushButton{Text: "Clean", ToolTipText: "Cleans output directories.", OnClicked: func() { mw.checkAutoClear(); mw.runClean() }},
							declarative.PushButton{Text: "Build", ToolTipText: "Builds the selected packs.", OnClicked: func() { mw.checkAutoClear(); mw.runBuild() }},
							declarative.PushButton{Text: "Build Release", ToolTipText: "Packages the build into release files.", OnClicked: func() { mw.checkAutoClear(); mw.runBuildRelease() }},
							declarative.PushButton{Text: "Install", ToolTipText: "Installs the selected packs.", OnClicked: func() { mw.checkAutoClear(); mw.runInstall() }},
							declarative.PushButton{Text: "Run Game", ToolTipText: "Launches the game executable.", OnClicked: func() { mw.checkAutoClear(); mw.runGame() }},
							declarative.PushButton{Text: "Uninstall", ToolTipText: "Removes currently installed mod files.", OnClicked: func() { mw.checkAutoClear(); mw.runUninstall() }},
							declarative.VSpacer{},
							declarative.PushButton{Text: "Abort", ToolTipText: "Abort current operations.", OnClicked: mw.runAbort},
						},
					},

					// --- Column 4: Options ---
					declarative.GroupBox{
						Title:         "Options",
						Layout:        declarative.Grid{Columns: 3, Spacing: 8}, // Grid layout for perfect alignment
						StretchFactor: 2,
						Children: []declarative.Widget{
							declarative.CheckBox{AssignTo: &mw.cbOptionAutoClear, Text: "Auto Clear Console", Checked: true, ToolTipText: "Clear the log before running a new action.", ColumnSpan: 3},
							declarative.CheckBox{AssignTo: &mw.cbOptionVerbose, Text: "Verbose Logging", Checked: true, ToolTipText: "Enable detailed logging output.", ColumnSpan: 3},
							declarative.CheckBox{AssignTo: &mw.cbOptionParallel, Text: "Parallel Build (Multithreaded)", Checked: true, ToolTipText: "Use multiple CPU cores for building.", ColumnSpan: 3},

							declarative.VSpacer{Size: 8, ColumnSpan: 3}, // Divider

							// Align inputs natively using the grid columns
							declarative.Label{Text: "Game Executable:"},
							declarative.ComboBox{
								AssignTo:     &mw.cmExe,
								Model:        []string{"generalszh.exe", "generalsv.exe"},
								CurrentIndex: 0,
								ToolTipText:  "Select the game executable to target.",
								ColumnSpan:   2,
							},

							declarative.Label{Text: "Game Directory:"},
							declarative.LineEdit{
								AssignTo:    &mw.leGameDir,
								ReadOnly:    true,
								ToolTipText: "Directory where the game is installed. Can be overridden manually.",
							},
							declarative.PushButton{
								Text:        "...",
								MaxSize:     declarative.Size{Width: 30},
								ToolTipText: "Browse for game directory.",
								OnClicked:   mw.selectGameDir,
							},

							declarative.Label{Text: "Project Directory:"},
							declarative.LineEdit{
								AssignTo:    &mw.leProjectDir,
								Text:        b.ProjectDir,
								ReadOnly:    true,
								ToolTipText: "Directory of the mod project configuration.",
							},
							declarative.PushButton{
								Text:        "...",
								MaxSize:     declarative.Size{Width: 30},
								ToolTipText: "Browse for project directory.",
								OnClicked:   mw.selectProjectDir,
							},

							declarative.VSpacer{ColumnSpan: 3}, // Anchors the Grid to the top
						},
					},
				},
			},

			// --- Text Log Area ---
			declarative.TextEdit{
				AssignTo:    &mw.teLog,
				ReadOnly:    true,
				VScroll:     true,
				MinSize:     declarative.Size{Height: 120},
				ToolTipText: "Operation logs and console output.",
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

	// Intercept Shift key to block range selection (Ctrl-multi-select BUT NO Shift-multi-select)
	mw.lbPacks.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if walk.ModifiersDown()&walk.ModShift != 0 {
			// Get item index from point (LB_ITEMFROMPOINT = 0x01A9)
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

	mw.Run()
}

// --- Logic & Event Handlers ---

func (mw *ModBuilderWindow) checkAutoClear() {
	if mw.cbOptionAutoClear.Checked() {
		mw.teLog.SetText("")
	}
}

func (mw *ModBuilderWindow) executeSequence() {
	mw.checkAutoClear()
	mw.builder.Parallel = mw.cbOptionParallel.Checked()

	if mw.cbSequenceSnapshot.Checked() {
		// To ensure a truly pristine snapshot, we uninstall the active mod FIRST.
		mw.builder.Uninstall(mw.cmExe.Text())
		mw.runSnapshot()
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

func (mw *ModBuilderWindow) runSnapshot() {
	gameDir := mw.builder.GetGameDir("", mw.cmExe.Text())
	if err := mw.builder.RefreshBaseline(gameDir); err != nil {
		mw.log(fmt.Sprintf("Error taking snapshot: %v", err))
	} else {
		mw.log("Vanilla Baseline Snapshot created successfully!")
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

	// Language Filtering Logic
	finalPacksToInstall := []BundlePack{}
	var lastLangPack *BundlePack

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
	mw.log("Generating Changelog...")
	if err := mw.builder.MakeChangeLog(); err != nil {
		mw.log(fmt.Sprintf("Changelog generation failed: %v", err))
	} else {
		mw.log("Changelog generated successfully.")
	}
}

func (mw *ModBuilderWindow) runAbort() {
	mw.log("Abort signaled (Background cancellation currently unhandled).")
}

func (mw *ModBuilderWindow) log(msg string) {
	mw.Synchronize(func() {
		mw.teLog.AppendText(msg + "\r\n")
	})
}

// --- Path Management ---

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
	// 1. Try to load from embedded PNG
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
