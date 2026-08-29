package config

import (
	"os"

	"github.com/joho/godotenv"
)

// dotenvFile is the file loaded into the process environment at startup when present.
const dotenvFile = ".env"

// LoadDotEnv loads key/value pairs from .env in the working directory into the process
// environment. Values already present in the environment are never overridden, so real
// environment variables and shell exports always win over the file. A missing .env is
// not an error.
func LoadDotEnv() error {
	info, err := os.Stat(dotenvFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	return godotenv.Load(dotenvFile)
}
