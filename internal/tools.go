package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

type ToolRunner struct {
	ToolsDir   string
	ProjectDir string
	Logger     func(string)
	Semaphore  chan struct{}
}

var DefaultTools = map[string]struct {
	URL  string
	Hash string
}{
	"crunch_x64.exe": {
		URL:  "https://github.com/TheSuperHackers/GeneralsTools/raw/main/Tools/crunch/v1.04/crunch_x64.exe",
		Hash: "8ae949bfc3e3e4a1717dca8845ce8ed480638de68cbf1d7cbe912e99e35ce06f",
	},
	"gametextcompiler.exe": {
		URL:  "https://github.com/TheSuperHackers/GeneralsTools/raw/main/Tools/gametextcompiler/v1.1/gametextcompiler.exe",
		Hash: "9c4c50f9c4829caff9b913bdd1a398a3a323ae9a1599723ff71ac675f6087467",
	},
	"generalsbigcreator.exe": {
		URL:  "https://github.com/TheSuperHackers/GeneralsTools/raw/main/Tools/generalsbigcreator/v1.3/generalsbigcreator.exe",
		Hash: "213ce479a033db949f19012a7e4270cf6c83a79282c1b1941f625b825c1451ed",
	},
	"7z.exe": {
		URL:  "https://github.com/TheSuperHackers/GeneralsTools/raw/main/Tools/7-Zip/7z1900-x64/7z.exe",
		Hash: "344f076bb1211cb02eca9e5ed2c0ce59bcf74ccbc749ec611538fa14ecb9aad2",
	},
	"7z.dll": {
		URL:  "https://github.com/TheSuperHackers/GeneralsTools/raw/main/Tools/7-Zip/7z1900-x64/7z.dll",
		Hash: "34ad9bb80fe8bf28171e671228eb5b64a55caa388c31cb8c0df77c0136735891",
	},
}

func NewToolRunner(toolsDir, projectDir string, logger func(string)) *ToolRunner {
	return &ToolRunner{ToolsDir: toolsDir, ProjectDir: projectDir, Logger: logger}
}

func (tr *ToolRunner) log(format string, a ...interface{}) {
	if tr.Logger != nil {
		tr.Logger(fmt.Sprintf(format, a...))
	}
}

func (tr *ToolRunner) run(cmdPath string, args ...string) error {
	if tr.Semaphore != nil {
		tr.Semaphore <- struct{}{}
		defer func() { <-tr.Semaphore }()
	}

	tr.log("Running: %s %v", filepath.Base(cmdPath), args)
	cmd := exec.Command(cmdPath, args...)
	cmd.Dir = tr.ProjectDir
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		tr.log("%s", string(output))
	}
	if err != nil {
		return fmt.Errorf("tool failed: %v", err)
	}
	return nil
}

func (tr *ToolRunner) RunBigCreator(args ...string) error {
	path, err := tr.getToolPath("generalsbigcreator.exe")
	if err != nil {
		return err
	}
	return tr.run(path, args...)
}

func (tr *ToolRunner) RunGameTextCompiler(args ...string) error {
	path, err := tr.getToolPath("gametextcompiler.exe")
	if err != nil {
		return err
	}
	return tr.run(path, args...)
}

func (tr *ToolRunner) RunCrunch(args ...string) error {
	path, err := tr.getToolPath("crunch_x64.exe")
	if err != nil {
		return err
	}
	return tr.run(path, args...)
}

func (tr *ToolRunner) Run7z(args ...string) error {
	// Ensure DLL is present first
	if _, err := tr.getToolPath("7z.dll"); err != nil {
		return err
	}
	path, err := tr.getToolPath("7z.exe")
	if err != nil {
		return err
	}
	return tr.run(path, args...)
}

func (tr *ToolRunner) getToolPath(toolName string) (string, error) {
	path := filepath.Join(tr.ToolsDir, toolName)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	tr.log("Tool %s missing, attempting to extract from binary...", toolName)
	data, err := getEmbeddedData(toolName)
	if err == nil {
		os.MkdirAll(tr.ToolsDir, 0755)
		os.WriteFile(path, data, 0755)
		return path, nil
	}

	tr.log("Tool %s not embedded, attempting to pull...", toolName)
	if info, ok := DefaultTools[toolName]; ok {
		if err := tr.DownloadFileWithVerify(info.URL, path, info.Hash); err != nil {
			return "", err
		}
		return path, nil
	}

	return "", fmt.Errorf("tool %s not found", toolName)
}

func (tr *ToolRunner) DownloadFileWithVerify(url, target, expectedHash string) error {
	tr.log("Downloading %s...", url)
	os.MkdirAll(filepath.Dir(target), 0755)
	
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		return err
	}

	if expectedHash != "" {
		if !tr.VerifyHash(target, expectedHash) {
			return fmt.Errorf("hash verification failed for %s", target)
		}
	}
	return nil
}

func (tr *ToolRunner) VerifyHash(path, expectedHash string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == expectedHash
}

var getEmbeddedData = func(_ string) ([]byte, error) {
	return nil, fmt.Errorf("not embedded")
}
