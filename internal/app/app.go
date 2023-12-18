package app

import (
	"go.uber.org/zap"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/logger"
	"github.com/bondar-aleksandr/cisco_ssh_client/internal/device"
	"path/filepath"
	"os"
	"gopkg.in/yaml.v3"
	"bufio"
)

type App struct {
	Logger *zap.SugaredLogger
	CmdCache map[string]*Commands
	ConfigPath string
	Config *config
}

func NewApp(cfgPath string) *App {
	app := &App{
		CmdCache: make(map[string]*Commands),
		ConfigPath: cfgPath,
	}
	// app.getLogger()
	app.Logger = logger.InitLogger(cfgPath)
	app.readConfig()
	app.prepareDirectory()
	return app
}

// type used for storing all commands from single command file
type Commands struct {
	Commands []string
}

func (c *Commands) Add(cmd string) {
	c.Commands = append(c.Commands, cmd)
}

// type for app-level config
type config struct {
	Client struct  {
		SSHTimeout int64 `yaml:"ssh_timeout"`
		LegacyKeyExchange string `yaml:"legacy_key_exchange"`
		LegacyAlgorithm string `yaml:"legacy_algorithm"`
	}
	Data struct {
		InputFolder  string `yaml:"input_folder"`
		DevicesData  string `yaml:"devices_data"`
		OutputFolder string `yaml:"output_folder"`
		ResultsData  string `yaml:"results_data"`
	}
}

// this func Unmarshals config.yml content to config variable
func(a *App) readConfig() {
	a.Logger.Info("Reading config...")

	f, err := os.Open(a.ConfigPath)
	if err != nil {
		a.Logger.Errorf("Cannot read app config file because of: %s", err)
		os.Exit(1)
	}
	defer f.Close()

	cfg := &config{}

	decoder := yaml.NewDecoder(f)
	// decoder.KnownFields(true)
	err = decoder.Decode(cfg)
	if err != nil {
		a.Logger.Errorf("Cannot parse app config file because of: %s", err)
		os.Exit(1)
	}
	a.Config = cfg
	a.Logger.Info("Reading config done")
	//TODO: print config parameters
}

// func receives list of Devices, walk through it, finds unique filenames, and populates
// cmdCache variable with mapping filename:Commands
func(a *App) BuildCmdCache(entries []*device.Device) {
	a.Logger.Info("Building cmd cache...")

	for _, entry := range entries {
		commandsFile, err := os.Open(filepath.Join(a.Config.Data.InputFolder, entry.CmdFile))
		if err != nil {
			a.Logger.Errorf("Unable to open commands file: %s", entry.CmdFile)
			os.Exit(1)
		}
		defer commandsFile.Close()

		// check whether info about entry.CmdFile is already in cmdCache map
		if _, ok := a.CmdCache[entry.CmdFile]; ok {
			continue
		}
		// commands parsing
		commands := Commands{}
		scanner := bufio.NewScanner(commandsFile)
		for scanner.Scan() {
			cmd := scanner.Text()
			commands.Add(cmd)
		}
		// add data to cache
		a.CmdCache[entry.CmdFile] = &commands
	}
	a.Logger.Info("Building cmd cache done")
}

// this func creates directory for storing outputs if it doesn't exists before
func(a *App) prepareDirectory() {
	//create folder for outputs if not exists
	a.Logger.Info("Creating output directory is not exists...")
	outDir := filepath.Join(a.Config.Data.OutputFolder)
	_, err := os.Stat(outDir)

	if os.IsNotExist(err) {
		errDir := os.MkdirAll(a.Config.Data.OutputFolder, os.ModePerm)
		if errDir != nil {
			a.Logger.Errorf("Cannot create directory for outputs because of: %q, exiting...", err)
			os.Exit(1)
		}
		a.Logger.Infof("Created output directory %q successfully", outDir)
	} else {
		a.Logger.Info("Output directory already there")
	}
}