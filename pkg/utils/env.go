package utils

import "os"

const DefaultEnvironment = "local"

func GetEnvironmentName() string {
	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = DefaultEnvironment
	}
	return environment
}
