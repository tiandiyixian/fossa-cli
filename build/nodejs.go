package build

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar"
	logging "github.com/op/go-logging"

	"github.com/fossas/fossa-cli/module"
)

var nodejsLogger = logging.MustGetLogger("nodejs")

// NodeModule implements Dependency for NodeJSBuilder.
type NodeModule struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Fetcher always returns npm for NodeModule. TODO: Support git and other
// dependency sources.
func (m NodeModule) Fetcher() string {
	return "npm" // TODO: support git and etc...
}

// Package returns the package name for NodeModule
func (m NodeModule) Package() string {
	return m.Name
}

// Revision returns the version for NodeModule
func (m NodeModule) Revision() string {
	return m.Version
}

// NodeJSBuilder implements Builder for Nodejs.
// These properties are public for the sake of serialization.
type NodeJSBuilder struct {
	NodeCmd     string
	NodeVersion string

	NpmCmd     string
	NpmVersion string

	YarnCmd     string
	YarnVersion string
}

// Initialize collects environment data for Nodejs builds
func (builder *NodeJSBuilder) Initialize() error {
	nodejsLogger.Debugf("Initializing Nodejs builder...")
	// Set NodeJS context variables
	nodeCmds := [3]string{os.Getenv("NODE_BINARY"), "node", "nodejs"}
	for i := 0; true; i++ {
		if i >= len(nodeCmds) {
			return errors.New("could not find Nodejs binary (try setting $NODE_BINARY)")
		}
		if nodeCmds[i] == "" {
			continue
		}

		nodeVersionOutput, err := exec.Command(nodeCmds[i], "-v").Output()
		if err == nil && nodeVersionOutput[0] == 'v' {
			builder.NodeVersion = strings.TrimSpace(string(nodeVersionOutput))[1:]
			builder.NodeCmd = nodeCmds[i]
			break
		}
	}

	// Set NPM context variables
	builder.NpmCmd = os.Getenv("NPM_BINARY")
	if builder.NpmCmd == "" {
		builder.NpmCmd = "npm"
	}

	npmVersionOutput, err := exec.Command(builder.NpmCmd, "-v").Output()
	if err == nil && len(npmVersionOutput) >= 5 {
		builder.NpmVersion = strings.TrimSpace(string(npmVersionOutput))
	}

	// Set Yarn context variables
	builder.YarnCmd = string(os.Getenv("YARN_BINARY"))
	if builder.YarnCmd == "" {
		builder.YarnCmd = "yarn"
	}
	yarnVersionOutput, err := exec.Command(builder.YarnCmd, "-v").Output()
	if err == nil && len(yarnVersionOutput) >= 5 {
		builder.YarnVersion = strings.TrimSpace(string(yarnVersionOutput))
	}

	if (builder.NpmCmd == "" || builder.NpmVersion == "") && (builder.YarnCmd == "" || builder.YarnVersion == "") {
		return errors.New("could not find NPM binary or Yarn binary (try setting $NPM_BINARY or $YARN_BINARY)")
	}

	nodejsLogger.Debugf("Initialized Nodejs builder: %#v", builder)

	return nil
}

func (builder *NodeJSBuilder) Build(m module.Module, force bool) error {
	nodejsLogger.Debugf("Running Nodejs build...")
	if force {
		nodejsLogger.Debug("`force` flag is set; clearing `node_modules`...")
		cmd := exec.Command("rm", "-rf", "node_modules")
		cmd.Dir = m.Dir
		_, err := cmd.Output()
		if err != nil {
			return err
		}
	}

	// Prefer Yarn where possible
	if _, err := os.Stat(filepath.Join(m.Dir, "yarn.lock")); err == nil {
		nodejsLogger.Debugf("Yarn lockfile detected.")
		if builder.YarnCmd == "" {
			return errors.New("Yarn lockfile found but could not find Yarn binary (try setting $YARN_BINARY)")
		}

		// TODO(xizhao): Verify compatible yarn versions
		nodejsLogger.Debugf("Running `yarn install --production --frozen-lockfile`.")
		cmd := exec.Command(builder.YarnCmd, "install", "--production", "--frozen-lockfile")
		cmd.Dir = m.Dir
		_, err := cmd.Output()
		return err
	}

	cmd := exec.Command(builder.NpmCmd, "install", "--production")
	cmd.Dir = m.Dir
	_, err := cmd.Output()
	return err
}

func (builder *NodeJSBuilder) Analyze(m module.Module, _ bool) ([]module.Dependency, error) {
	nodejsLogger.Debugf("Running analysis on Nodejs module...")
	nodeModules, err := doublestar.Glob(filepath.Join(m.Dir, "**", "node_modules", "*", "package.json"))
	if err != nil {
		return nil, err
	}
	nodejsLogger.Debugf("Found %#v modules from globstar.", len(nodeModules))

	var wg sync.WaitGroup
	dependencies := make([]NodeModule, len(nodeModules))
	wg.Add(len(nodeModules))

	for i := 0; i < len(nodeModules); i++ {
		go func(modulePath string, index int, wg *sync.WaitGroup) {
			defer wg.Done()

			dependencyManifest, err := ioutil.ReadFile(modulePath)
			if err != nil {
				nodejsLogger.Warningf("Error parsing Module: %#v", modulePath)
				return
			}

			// Write directly to a reserved index for thread safety
			json.Unmarshal(dependencyManifest, &dependencies[index])
		}(nodeModules[i], i, &wg)
	}

	wg.Wait()

	var deps []module.Dependency
	for i := 0; i < len(dependencies); i++ {
		deps = append(deps, dependencies[i])
	}

	return deps, nil
}

func (builder *NodeJSBuilder) IsBuilt(m module.Module, _ bool) (bool, error) {
	nodeModulesPath := filepath.Join(m.Dir, "node_modules")
	nodejsLogger.Debugf("Checking node_modules at %#v", nodeModulesPath)
	// TODO: Check if the installed modules are consistent with what's in the
	// actual manifest.
	if _, err := os.Stat(nodeModulesPath); err == nil {
		return true, nil
	}
	return false, nil
}

func (builder *NodeJSBuilder) IsModule(target string) (bool, error) {
	return false, errors.New("IsModule is not implemented for NodeJSBuilder")
}

func (builder *NodeJSBuilder) InferModule(target string) (module.Module, error) {
	return module.Module{}, errors.New("InferModule is not implemented for NodeJSBuilder")
}