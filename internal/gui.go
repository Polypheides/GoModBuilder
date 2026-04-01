package internal

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
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

	// Path and Execution Controls
	cmProjectDir *walk.ComboBox
	cmGameDir    *walk.ComboBox
	cmExe        *walk.ComboBox
	leLaunchArgs *walk.LineEdit

	// Data Models
	packModel      []string
	exeModel       []string
	projectHistory []string
	gameDirHistory []string
	projectModel   []string
	gameDirModel   []string

	updatingUI bool        // Flag to prevent event loops during model refreshes
	logChan    chan string // Asynchronous logger channel
}

// Find the Run function and replace it with:

func Run(items *ModBundleItems, packs *ModBundlePacks, b *ModBuilder) {
	fmt.Println("Initializing GUI...")
	mw := new(ModBuilderWindow)
	mw.items = items
	mw.packs = packs
	mw.builder = b
	b.Logger = mw.log
	mw.packModel = make([]string, len(packs.Bundles.Packs))
	for i, p := range packs.Bundles.Packs {
		mw.packModel[i] = p.Name
	}

	// Load App Settings (Global config should be in the App Root, not the project folder)
	exePath, err := os.Executable()
	var cwd string
	if err == nil {
		cwd = filepath.Dir(exePath)
	} else {
		cwd, _ = os.Getwd()
	}
	settingsPath := filepath.Join(cwd, "ModBuilderSettings.json")
	settings, _ := LoadAppSettings(settingsPath)

	initialExe := "generalszh.exe"
	initialArgs := "-win -quickstart"
	if settings != nil {
		if settings.SelectedExe != "" {
			initialExe = settings.SelectedExe
		}
		if settings.LaunchArgs != "" {
			initialArgs = settings.LaunchArgs
		}
		mw.projectHistory = settings.ProjectHistory
		mw.gameDirHistory = settings.GameDirHistory
	}

	// Fallbacks if history is empty
	if len(mw.projectHistory) == 0 {
		mw.projectHistory = []string{b.ProjectDir}
		if b.ProjectDir == "" {
			mw.projectHistory = []string{cwd}
		}
	}
	if len(mw.gameDirHistory) == 0 {
		detected := b.GetGameDir("", initialExe)
		mw.gameDirHistory = []string{detected}
	}

	mw.builder.LoadBaseline() // Load the baseline for the current game dir

	if err := (declarative.MainWindow{
		AssignTo: &mw.MainWindow,
		Title:    "Go Mod Builder v1.1 by Polypheides",
		Icon:     mw.getAppIcon(),
		Size:     declarative.Size{Width: 950, Height: 550},
		MinSize:  declarative.Size{Width: 850, Height: 450},
		Font:     declarative.Font{Family: "Segoe UI", PointSize: 9},
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
								Model:          mw.packModel,
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
							declarative.PushButton{Text: "Snapshot", ToolTipText: "Captures a vanilla baseline of the game directory.", OnClicked: func() { mw.prepareAction(); mw.runSnapshot() }},
							declarative.PushButton{Text: "Changelog", ToolTipText: "Generates the project changelog.", OnClicked: func() { mw.prepareAction(); mw.runMakeChangeLog() }},
							declarative.PushButton{Text: "Clean", ToolTipText: "Cleans output directories.", OnClicked: func() { mw.prepareAction(); mw.runClean() }},
							declarative.PushButton{Text: "Build", ToolTipText: "Builds the selected packs.", OnClicked: func() { mw.prepareAction(); mw.runBuild() }},
							declarative.PushButton{Text: "Build Release", ToolTipText: "Packages the build into release files.", OnClicked: func() { mw.prepareAction(); mw.runBuildRelease() }},
							declarative.PushButton{Text: "Install", ToolTipText: "Installs the selected packs.", OnClicked: func() { mw.prepareAction(); mw.runInstall() }},
							declarative.PushButton{Text: "Run Game", ToolTipText: "Launches the game executable.", OnClicked: func() { mw.prepareAction(); mw.runGame() }},
							declarative.PushButton{Text: "Uninstall", ToolTipText: "Removes currently installed mod files.", OnClicked: func() { mw.prepareAction(); mw.runUninstall() }},
							declarative.VSpacer{},
							declarative.PushButton{Text: "Abort", ToolTipText: "Abort current operations.", OnClicked: mw.runAbort},
						},
					},

					// --- Column 4: Options ---
					declarative.GroupBox{
						Title:         "Options",
						Layout:        declarative.Grid{Columns: 2, Spacing: 8}, // Grid layout for perfect alignment
						StretchFactor: 3,
						Children: []declarative.Widget{
							declarative.CheckBox{AssignTo: &mw.cbOptionAutoClear, Text: "Auto Clear Console", Checked: true, ToolTipText: "Clear the log before running a new action.", ColumnSpan: 2},
							declarative.CheckBox{AssignTo: &mw.cbOptionVerbose, Text: "Verbose Logging", Checked: true, ToolTipText: "Enable detailed logging output.", ColumnSpan: 2},
							declarative.CheckBox{AssignTo: &mw.cbOptionParallel, Text: "Parallel Build (Multithreaded)", Checked: true, ToolTipText: "Use multiple CPU cores for building.", ColumnSpan: 2},
							declarative.VSpacer{Size: 8, ColumnSpan: 2},

							declarative.Label{Text: "Project Directory:"},
							declarative.ComboBox{
								AssignTo:    &mw.cmProjectDir,
								Editable:    true,
								MinSize:     declarative.Size{Width: 200}, // Prevent window stretching
								ToolTipText: "Directory of the mod project configuration.",
							},

							declarative.Label{Text: "Game Directory:"},
							declarative.ComboBox{
								AssignTo:    &mw.cmGameDir,
								Editable:    true,
								MinSize:     declarative.Size{Width: 200}, // Prevent window stretching
								ToolTipText: "Directory where the game is installed.",
							},

							declarative.Label{Text: "Game Executable:"},
							declarative.ComboBox{
								AssignTo:    &mw.cmExe,
								ToolTipText: "Select the game executable to target.",
								ColumnSpan:  2,
							},

							declarative.Label{Text: "Launch Arguments:"},
							declarative.LineEdit{
								AssignTo:    &mw.leLaunchArgs,
								Text:        initialArgs,
								ToolTipText: "Custom command line arguments for the game.",
								ColumnSpan:  2,
							},
							declarative.VSpacer{ColumnSpan: 2}, // Anchors the Grid to the top
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

	// Initialize batched background logger
	mw.logChan = make(chan string, 10000)
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		var batch []string
		for {
			select {
			case msg, ok := <-mw.logChan:
				if !ok {
					return
				}
				batch = append(batch, msg)
			case <-ticker.C:
				if len(batch) > 0 {
					text := strings.Join(batch, "\r\n") + "\r\n"
					batch = nil // clear batch
					if mw.MainWindow != nil {
						mw.Synchronize(func() {
							if mw.teLog != nil {
								mw.teLog.AppendText(text)
							}
						})
					}
				}
			}
		}
	}()

	mw.updatingUI = true

	// Fast setup: we do NOT call mw.reDiscover() here because DiscoverConfigs
	// already executed in main.go! Just populate the models directly.
	mw.refreshProjectModel()
	mw.refreshGameDirModel()
	mw.updateExeList(mw.builder.CustomGameDir)

	// Set initial executable selection
	exeIdx := -1
	for i, name := range mw.exeModel {
		if strings.EqualFold(name, initialExe) {
			exeIdx = i
			break
		}
	}
	if exeIdx >= 0 {
		mw.cmExe.SetCurrentIndex(exeIdx)
	} else if len(mw.exeModel) > 0 {
		mw.cmExe.SetCurrentIndex(0)
	}
	mw.updatingUI = false

	// Attach Event Listeners
	mw.cmProjectDir.CurrentIndexChanged().Attach(mw.onProjectDirChanged)
	mw.cmGameDir.CurrentIndexChanged().Attach(mw.onGameDirChanged)
	mw.cmExe.CurrentIndexChanged().Attach(mw.saveSettings)
	mw.leLaunchArgs.TextChanged().Attach(mw.saveSettings)

	// Intercept Shift key to block range selection (Ctrl-multi-select BUT NO Shift-multi-select)
	mw.lbPacks.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if walk.ModifiersDown()&walk.ModShift != 0 {
			// Get item index from point (LB_ITEMFROMPOINT = 0x01A9)
			res := win.SendMessage(mw.lbPacks.Handle(), 0x01A9, 0, uintptr(win.MAKELONG(uint16(x), uint16(y))))
			itemIdx := int(win.LOWORD(uint32(res)))
			if win.HIWORD(uint32(res)) == 0 && itemIdx >= 0 {
				time.AfterFunc(10*time.Millisecond, func() {
					mw.Synchronize(func() { mw.lbPacks.SetSelectedIndexes([]int{itemIdx}) })
				})
			}
		}
	})

	mw.Run()
}

// --- Logic & Event Handlers ---

func (mw *ModBuilderWindow) prepareAction() {
	if mw.cbOptionAutoClear.Checked() {
		mw.teLog.SetText("")
	}
	mw.commitPathsToHistory()
}

func (mw *ModBuilderWindow) executeSequence() {
	mw.prepareAction()
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
		allSelectedPacks = append(allSelectedPacks, mw.packs.Bundles.Packs[idx])
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
	args := mw.leLaunchArgs.Text()
	if exeName == "" {
		exeName = "generalszh.exe"
	}
	mw.log(fmt.Sprintf("Launching %s...", exeName))

	if err := mw.builder.RunGame("_absInstallDir", exeName, "", args); err != nil {
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
	if mw.logChan != nil {
		mw.logChan <- msg
	} else {
		fmt.Println(msg)
	}
}

// --- Path Management ---

func shortenPath(p string) string {
	p = filepath.Clean(p)
	if len(p) <= 45 {
		return p
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	if len(parts) < 3 {
		return p[:20] + "..." + p[len(p)-20:]
	}
	return parts[0] + "/.../" + parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func addUniqueToHistory(history []string, path string) []string {
	if path == "" || path == "... (Browse)" {
		return history
	}
	path = filepath.Clean(filepath.FromSlash(path))
	res := []string{path}
	for _, p := range history {
		if !strings.EqualFold(p, path) {
			res = append(res, p)
		}
	}
	if len(res) > 10 {
		res = res[:10]
	}
	return res
}

func (mw *ModBuilderWindow) commitPathsToHistory() {
	pDir := mw.cmProjectDir.Text()
	pChanged := false
	if pDir != "" && pDir != "... (Browse)" && !strings.EqualFold(pDir, mw.builder.ProjectDir) {
		mw.projectHistory = addUniqueToHistory(mw.projectHistory, pDir)
		mw.builder.SetProjectDir(mw.projectHistory[0])
		pChanged = true
	}

	gDir := mw.cmGameDir.Text()
	gChanged := false
	if gDir != "" && gDir != "... (Browse)" && !strings.EqualFold(gDir, mw.builder.CustomGameDir) {
		mw.gameDirHistory = addUniqueToHistory(mw.gameDirHistory, gDir)
		mw.builder.CustomGameDir = mw.gameDirHistory[0]
		gChanged = true
	}

	if pChanged || gChanged {
		mw.updatingUI = true

		// Preserve user's selection
		var oldSelections []int
		if mw.lbPacks != nil {
			oldSelections = mw.lbPacks.SelectedIndexes()
		}

		if pChanged {
			mw.reDiscover()
		} else {
			mw.refreshGameDirModel()
			mw.updateExeList(mw.builder.CustomGameDir)
		}

		// Carefully restore selected UI indexes post-update
		if len(oldSelections) > 0 && mw.lbPacks != nil && mw.packModel != nil {
			validSelections := []int{}
			for _, idx := range oldSelections {
				if idx < len(mw.packModel) {
					validSelections = append(validSelections, idx)
				}
			}
			if len(validSelections) > 0 {
				mw.lbPacks.SetSelectedIndexes(validSelections)
			}
		}

		mw.updatingUI = false
		mw.saveSettings()
	}
}

func (mw *ModBuilderWindow) refreshProjectModel() {
	mw.projectModel = make([]string, len(mw.projectHistory))
	for i, p := range mw.projectHistory {
		mw.projectModel[i] = p // Display exactly as saved (absolute path)
	}
	mw.projectModel = append(mw.projectModel, "... (Browse)")
	if mw.cmProjectDir != nil {
		mw.cmProjectDir.SetModel(mw.projectModel)
		if len(mw.projectHistory) > 0 {
			mw.cmProjectDir.SetCurrentIndex(0)
		}
	}
}

func (mw *ModBuilderWindow) refreshGameDirModel() {
	mw.gameDirModel = make([]string, len(mw.gameDirHistory))
	for i, p := range mw.gameDirHistory {
		mw.gameDirModel[i] = p // Display exactly as saved (absolute path)
	}
	mw.gameDirModel = append(mw.gameDirModel, "... (Browse)")
	if mw.cmGameDir != nil {
		mw.cmGameDir.SetModel(mw.gameDirModel)
		if len(mw.gameDirHistory) > 0 {
			mw.cmGameDir.SetCurrentIndex(0)
		}
	}
}

func (mw *ModBuilderWindow) onProjectDirChanged() {
	if mw.updatingUI {
		return
	}
	idx := mw.cmProjectDir.CurrentIndex()
	if idx == len(mw.projectModel)-1 || mw.cmProjectDir.Text() == "... (Browse)" {
		mw.selectProjectDir()
		return
	}
	if idx >= 0 && idx < len(mw.projectHistory) {
		selected := mw.projectHistory[idx]
		mw.projectHistory = addUniqueToHistory(mw.projectHistory, selected)
		mw.builder.SetProjectDir(selected)
		mw.updatingUI = true
		mw.refreshProjectModel()
		mw.updatingUI = false
		mw.reDiscover()
		mw.saveSettings()
	}
}

func (mw *ModBuilderWindow) onGameDirChanged() {
	if mw.updatingUI {
		return
	}
	idx := mw.cmGameDir.CurrentIndex()
	if idx == len(mw.gameDirModel)-1 || mw.cmGameDir.Text() == "... (Browse)" {
		mw.selectGameDir()
		return
	}
	if idx >= 0 && idx < len(mw.gameDirHistory) {
		selected := mw.gameDirHistory[idx]
		mw.gameDirHistory = addUniqueToHistory(mw.gameDirHistory, selected)
		mw.builder.CustomGameDir = selected
		mw.builder.LoadBaseline() // Reload baseline for new game dir
		mw.updatingUI = true
		mw.refreshGameDirModel()
		mw.updateExeList(selected)
		mw.updatingUI = false
		mw.saveSettings()
	}
}

func (mw *ModBuilderWindow) updateExeList(dir string) {
	files, err := os.ReadDir(dir)
	if err != nil {
		mw.log(fmt.Sprintf("Error scanning directory for executables: %v", err))
		return
	}
	var exes []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".exe") {
			exes = append(exes, f.Name())
		}
	}
	if len(exes) == 0 {
		exes = []string{"generalszh.exe", "generals.exe"}
	}

	mw.exeModel = exes
	if mw.cmExe != nil {
		current := mw.cmExe.Text()
		mw.cmExe.SetModel(mw.exeModel)
		// Try to keep previous selection
		idx := -1
		for i, name := range exes {
			if strings.EqualFold(name, current) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			mw.cmExe.SetCurrentIndex(idx)
		} else if len(exes) > 0 {
			mw.cmExe.SetCurrentIndex(0)
		}
	}
}

func (mw *ModBuilderWindow) selectGameDir() {
	dlg := new(walk.FileDialog)
	if ok, _ := dlg.ShowBrowseFolder(mw); ok {
		mw.gameDirHistory = addUniqueToHistory(mw.gameDirHistory, dlg.FilePath)
		mw.builder.CustomGameDir = mw.gameDirHistory[0]
		mw.updatingUI = true
		mw.refreshGameDirModel()
		mw.updateExeList(mw.builder.CustomGameDir)
		mw.updatingUI = false
		mw.saveSettings()
	} else {
		// Reset to previous top if cancelled to remove "..." from the visual box
		mw.updatingUI = true
		if len(mw.gameDirHistory) > 0 && mw.cmGameDir != nil {
			mw.cmGameDir.SetCurrentIndex(0)
		}
		mw.updatingUI = false
	}
}

func (mw *ModBuilderWindow) selectProjectDir() {
	dlg := new(walk.FileDialog)
	if ok, _ := dlg.ShowBrowseFolder(mw); ok {
		mw.projectHistory = addUniqueToHistory(mw.projectHistory, dlg.FilePath)
		mw.builder.SetProjectDir(mw.projectHistory[0])
		mw.updatingUI = true
		mw.refreshProjectModel()
		mw.updatingUI = false
		mw.reDiscover()
		mw.saveSettings()
	} else {
		// Reset to previous top if cancelled to remove "..." from the visual box
		mw.updatingUI = true
		if len(mw.projectHistory) > 0 && mw.cmProjectDir != nil {
			mw.cmProjectDir.SetCurrentIndex(0)
		}
		mw.updatingUI = false
	}
}

func (mw *ModBuilderWindow) saveSettings() {
	if mw.cmExe == nil || mw.leLaunchArgs == nil || mw.cmProjectDir == nil || mw.cmGameDir == nil {
		return
	}
	settings := &AppSettings{
		CustomGameDir:  mw.builder.CustomGameDir,
		SelectedExe:    mw.cmExe.Text(),
		LaunchArgs:     mw.leLaunchArgs.Text(),
		ProjectHistory: mw.projectHistory,
		GameDirHistory: mw.gameDirHistory,
	}
	// Save App Settings (Global config should be in the App Root, not the project folder)
	exePath, err := os.Executable()
	var cwd string
	if err == nil {
		cwd = filepath.Dir(exePath)
	} else {
		cwd, _ = os.Getwd()
	}
	settingsPath := filepath.Join(cwd, "ModBuilderSettings.json")
	SaveAppSettings(settingsPath, settings)
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

	mw.packModel = make([]string, len(packs.Bundles.Packs))
	for i, p := range packs.Bundles.Packs {
		mw.packModel[i] = p.Name
	}

	if mw.lbPacks != nil {
		mw.lbPacks.SetModel(mw.packModel)
		// Selection is now handled dynamically in commitPathsToHistory
		// so it will no longer arbitrarily clear
	}

	mw.refreshProjectModel()
	mw.refreshGameDirModel()
	mw.updateExeList(mw.builder.CustomGameDir)

	mw.log(fmt.Sprintf("Discovery complete: %d items, %d packs in %s", len(items.Bundles.Items), len(packs.Bundles.Packs), configDir))
}

func (mw *ModBuilderWindow) getAppIcon() interface{} {
	if len(IconPNG) > 0 {
		img, _, err := image.Decode(bytes.NewReader(IconPNG))
		if err == nil {
			if icon, err := walk.NewIconFromImage(img); err == nil {
				return icon
			}
		}
	}
	return nil
}
