package controller

import (
	"fmt"
	"path/filepath"
)

func syncTerraformS3Bucket(s3Url, accessKeyID, secretAccessKey, bucketName string) bool {
	// 1. Crea un client S3 utilizzando le credenziali fornite

	allDirs, err := listAllFilesInDir("/terraform-templates")
	if err != nil {
		fmt.Printf("Errore durante la lettura dei file: %v\n", err)
		return false
	}

	for _, fileSorgente := range allDirs {

		content, err := readFileContent(fileSorgente)
		if err != nil {
			fmt.Printf("Errore durante la lettura del file: %v\n", err)
			return false
		}

		fileName := "rules-templates/" + filepath.Base(fileSorgente)

		err = writeS3File(fileName, content, bucketName, accessKeyID, secretAccessKey, s3Url, true)
		if err != nil {
			fmt.Printf("Errore durante la scrittura su S3: %v\n", err)
			return false
		}
	}

	return true
}
