package settings

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

type Settings struct {
	DatabaseType     string `json:"database_type"`
	ConnectionString string `json:"connection_string"`
}

func LoadSettings() *Settings {
	var settings Settings

	cwd, err := os.Getwd()
	if err != nil {
		log.Print(err)
	}

	settingsPath := filepath.Join(cwd, "settings.json")

	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		fmt.Println(err)
		return &settings
	}
	jsonFile, err := os.Open(settingsPath)

	if err != nil {
		log.Print(err)
	}

	byteValue, _ := ioutil.ReadAll(jsonFile)

	json.Unmarshal(byteValue, &settings)
	defer jsonFile.Close()
	return &settings
}
