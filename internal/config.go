package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type BundleItem struct {
	Name                       string       `json:"name"`
	NamePrefix                 string       `json:"namePrefix,omitempty"`
	NameSuffix                 string       `json:"nameSuffix,omitempty"`
	Big                        bool         `json:"big"`
	BigSuffix                  string       `json:"bigSuffix,omitempty"`
	SetGameLanguageOnInstall   string       `json:"setGameLanguageOnInstall,omitempty"`
	Files                      []BundleFile `json:"files"`
	OnPreBuild                 *EventConfig `json:"onPreBuild,omitempty"`
	OnBuild                    *EventConfig `json:"onBuild,omitempty"`
	OnPostBuild                *EventConfig `json:"onPostBuild,omitempty"`
	OnFinishBuildRawBundleItem *EventConfig `json:"onFinishBuildRawBundleItem,omitempty"`
}

type BundleFile struct {
	SourceParent       string                 `json:"sourceParent"`
	Source             string                 `json:"source,omitempty"`
	Target             string                 `json:"target,omitempty"`
	SourceList         []string               `json:"sourceList,omitempty"`
	SourceTargetList   []SourceTarget         `json:"sourceTargetList,omitempty"`
	RegistryList       []string               `json:"registryList,omitempty"`
	Params             map[string]interface{} `json:"params,omitempty"`
	ExcludeMarkersList [][]string             `json:"excludeMarkersList,omitempty"`
}

type SourceTarget struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type EventConfig struct {
	Script   string                 `json:"script"`
	Function string                 `json:"function,omitempty"`
	Kwargs   map[string]interface{} `json:"kwargs,omitempty"`
}

type ModBundleItems struct {
	Bundles struct {
		Version     int          `json:"version"`
		ItemsPrefix string       `json:"itemsPrefix"`
		ItemsSuffix string       `json:"itemsSuffix"`
		Items       []BundleItem `json:"items"`
	} `json:"bundles"`
}

type BundlePack struct {
	Name                     string       `json:"name"`
	ItemNames                []string     `json:"itemNames"`
	SetGameLanguageOnInstall string       `json:"setGameLanguageOnInstall,omitempty"`
	OnPreBuild               *EventConfig `json:"onPreBuild,omitempty"`
	OnRelease                *EventConfig `json:"onRelease,omitempty"`
	OnInstall                *EventConfig `json:"onInstall,omitempty"`
	OnRun                    *EventConfig `json:"onRun,omitempty"`
	OnUninstall              *EventConfig `json:"onUninstall,omitempty"`
}

type ModBundlePacks struct {
	Bundles struct {
		Version     int          `json:"version"`
		PacksPrefix string       `json:"packsPrefix"`
		PacksSuffix string       `json:"packsSuffix"`
		Packs       []BundlePack `json:"packs"`
	} `json:"bundles"`
}

func (m *ModBundleItems) Merge(other *ModBundleItems) {
	m.Bundles.Items = append(m.Bundles.Items, other.Bundles.Items...)
}

func (m *ModBundlePacks) Merge(other *ModBundlePacks) {
	m.Bundles.Packs = append(m.Bundles.Packs, other.Bundles.Packs...)
}

func LoadBundleItems(path string) (*ModBundleItems, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config ModBundleItems
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func LoadBundlePacks(path string) (*ModBundlePacks, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config ModBundlePacks
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

type ModJsonFiles struct {
	Build struct {
		Version int      `json:"version"`
		Files   []string `json:"files"`
	} `json:"build"`
}

func LoadJsonFilesList(path string) (*ModJsonFiles, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config ModJsonFiles
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

type ModChangeLog struct {
	Changelog struct {
		Version int            `json:"version"`
		Records []ChangeRecord `json:"records"`
	} `json:"changelog"`
}

type ChangeRecord struct {
	SourceList       []string                 `json:"sourceList"`
	TargetList       []string                 `json:"targetList"`
	SortList         []map[string]interface{} `json:"sortList"`
	IncludeLabelList []string                 `json:"includeLabelList"`
	ExcludeLabelList []string                 `json:"excludeLabelList"`
}

func DiscoverConfigs(projectDir string) (*ModBundleItems, *ModBundlePacks, error) {
	items := &ModBundleItems{}
	packs := &ModBundlePacks{}

	jsonFilesPath := filepath.Join(projectDir, "ModJsonFiles.json")
	if _, err := os.Stat(jsonFilesPath); os.IsNotExist(err) {
		// Fallback to legacy discovery if ModJsonFiles.json is missing
		return discoverConfigsLegacy(projectDir)
	}

	configList, err := LoadJsonFilesList(jsonFilesPath)
	if err != nil {
		return nil, nil, err
	}

	for _, fileName := range configList.Build.Files {
		path := filepath.Join(projectDir, fileName)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		lowerName := strings.ToLower(fileName)
		if strings.Contains(lowerName, "items") {
			it, err := LoadBundleItems(path)
			if err == nil {
				items.Merge(it)
			}
		} else if strings.Contains(lowerName, "packs") {
			pk, err := LoadBundlePacks(path)
			if err == nil {
				packs.Merge(pk)
			}
		}
	}

	return items, packs, nil
}

func discoverConfigsLegacy(projectDir string) (*ModBundleItems, *ModBundlePacks, error) {
	items := &ModBundleItems{}
	packs := &ModBundlePacks{}

	err := filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return filepath.SkipDir // If we can't access it, just skip it!
		}

		// Skip hidden/system folders (like $Recycle.Bin or .git)
		if info.IsDir() && (strings.HasPrefix(info.Name(), "$") || strings.HasPrefix(info.Name(), ".")) {
			return filepath.SkipDir
		}

		name := strings.ToLower(filepath.Base(path))
		if strings.Contains(name, "bundle") && strings.Contains(name, "items") && strings.HasSuffix(name, ".json") {
			it, err := LoadBundleItems(path)
			if err == nil {
				items.Merge(it)
			}
		} else if strings.Contains(name, "bundle") && strings.Contains(name, "packs") && strings.HasSuffix(name, ".json") {
			pk, err := LoadBundlePacks(path)
			if err == nil {
				packs.Merge(pk)
			}
		}
		return nil
	})

	return items, packs, err
}

type ModFolders struct {
	Folders struct {
		Version    int    `json:"version"`
		ReleaseDir string `json:"releaseDir"`
		BuildDir   string `json:"buildDir"`
		GameDir    string `json:"gameDir,omitempty"`
	} `json:"folders"`
}

func LoadFoldersConfig(path string) (*ModFolders, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config ModFolders
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func LoadChangeLogConfig(path string) (*ModChangeLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config ModChangeLog
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

type InstalledFile struct {
	Target string `json:"target"`
	Backup string `json:"backup,omitempty"`
}

type InstalledState struct {
	Files []InstalledFile `json:"files"`
}

type AppSettings struct {
	CustomGameDir string `json:"customGameDir"`
	SelectedExe   string `json:"selectedExe"`
	LaunchArgs    string `json:"launchArgs"`
}

func LoadAppSettings(path string) (*AppSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config AppSettings
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func SaveAppSettings(path string, settings *AppSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
