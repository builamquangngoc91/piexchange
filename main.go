package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
)

type EmailService interface {
	SendEmails(context.Context, []Email) (map[string]string, error) 
	GetName() string
	GetCurrentStatus() bool
}

type Email struct {
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Subject  string `json:"subject"`
	MineType string `json:"mineType"`
	Body     string `json:"body"`
}

type CustomerInfo map[string]string

func main() {
	templateFileStr := flag.String("templateFile", "", "path to template file")
	customersFileStr := flag.String("customersFile", "", "path to customer file")
	emailOutputsFileStr := flag.String("emailOutputsFile", "", "path to email outputs file")
	errorFileStr := flag.String("errorsFile", "", "path to errors file")
	emailServiceStr := flag.String("emailServiceStr", "", "choose email service to send email")
	flag.Parse()

	if templateFileStr == nil || strings.TrimSpace(*templateFileStr) == "" {
		fmt.Println("templateFile can't be empty")
	}
	if customersFileStr == nil || strings.TrimSpace(*customersFileStr) == "" {
		fmt.Println("customersFile can't be empty")
	}
	if emailOutputsFileStr == nil || strings.TrimSpace(*emailOutputsFileStr) == "" {
		fmt.Println("emailOutputsFile can't be empty")
	}
	if errorFileStr == nil || strings.TrimSpace(*errorFileStr) == "" {
		fmt.Println("errorFile can't be empty")
	}

	var customerInfos []CustomerInfo
	var customerInfoErrors [][]string
	{
		// Read customers
		customersFile, err := os.Open(*customersFileStr)
		if err != nil {
			log.Fatal(err)
		}

		// remember to close the file at the end of the program
		defer customersFile.Close()
		csvReader := csv.NewReader(customersFile)
		data, err := csvReader.ReadAll()
		if err != nil {
			fmt.Println(fmt.Errorf("csvReader.ReadAll: %w", err).Error())
		}

		emailIndex := -1
		for col := 0; col < len(data[0]); col++ {
			if data[0][col] == "EMAIL" {
				emailIndex = col
			}
		}
		if emailIndex == -1 {
			panic("email must be declared in customer file")
		}

		customerInfoErrors = append(customerInfoErrors, data[0])
		// convert 2d array to hashmap
		for row := 1; row < len(data); row++ {
			customerInfo := make(map[string]string)
			for col := 0; col < len(data[row]); col++ {
				header := data[0][col]
				customerInfo[header] = data[row][col]

			}

			email := data[row][emailIndex]
			if email != "" {
				customerInfos = append(customerInfos, customerInfo)
			} else {
				customerInfoErrors = append(customerInfoErrors, data[row])
			}
		}
	}

	var emailTemplate Email
	{
		jsonFile, err := os.Open(*templateFileStr)
		if err != nil {
			fmt.Println(err)
		}

		defer jsonFile.Close()

		byteValue, _ := io.ReadAll(jsonFile)

		json.Unmarshal(byteValue, &emailTemplate)
	}

	emails := make([]Email, 0, len(customerInfos))
	for _, customerInfo := range customerInfos {
		body := parseTemplateWithValue(emailTemplate.Body, customerInfo)

		email := Email{
			From:     emailTemplate.From,
			To:       customerInfo["EMAIL"],
			Subject:  emailTemplate.Subject,
			MineType: emailTemplate.MineType,
			Body:     body,
		}
		emails = append(emails, email)
	}

	// write errors to file
	{
		if len(customerInfoErrors) > 0 {
			f, err := os.Create(*errorFileStr)
			defer f.Close()

			if err != nil {

				log.Fatalln("failed to open file", err)
			}

			w := csv.NewWriter(f)
			err = w.WriteAll(customerInfoErrors) // calls Flush internally

			if err != nil {
				log.Fatal(err)
			}
		}
	}

	// write result to file
	{
		emailsJson, err := json.MarshalIndent(emails, "", " ")
		if err != nil {
			panic(fmt.Errorf("json.Marshal: %w", err).Error())
		}

		if err := ioutil.WriteFile(fmt.Sprintf("%s//emails.json", *emailOutputsFileStr), emailsJson, 0644); err != nil {
			panic(fmt.Errorf("ioutil.WriteFile: %w", err).Error())
		}
	}

	emailServiceMap := make(map[string]EmailService)
	if emailServiceStr != nil {
		emailService := emailServiceMap[*emailServiceStr]

		if emailService.GetCurrentStatus() {
			result, err := emailService.SendEmails(context.Background(), emails)
			if err != nil {
				return
			}

			fmt.Println(result)
		}
 	}
}

type PlaceHolderFunc func() string

func convertToTemplateWithFormatSpecifiersAndPlaceHolders(template string) (templateWithFormatSpecifiers string, placeHolders []string) {
	var (
		templateBuilder strings.Builder
		isPlaceHolder   bool
		i               int
		placeHolder     strings.Builder
	)

	for i < len(template) {
		if !isPlaceHolder {
			if i+1 < len(template) && template[i:i+2] == "{{" {
				isPlaceHolder = true
				i += 2
				placeHolder.Reset()
				continue
			}

			if err := templateBuilder.WriteByte(template[i]); err != nil {
				panic(fmt.Errorf("b.WriteByte: %w", err).Error())
			}
			i++
			continue
		}

		if i+1 < len(template) && template[i:i+2] == "}}" {
			if _, err := templateBuilder.WriteString("%s"); err != nil {
				panic(fmt.Errorf("b.WriteString: %w", err).Error())
			}

			placeHolders = append(placeHolders, placeHolder.String())
			isPlaceHolder = false
			i += 2
			continue
		}

		if err := placeHolder.WriteByte(template[i]); err != nil {
			panic(fmt.Errorf("placeHolder.WriteByte: %w", err).Error())
		}
		i++
	}

	return templateBuilder.String(), placeHolders
}

func parseTemplateWithValue(template string, placeHolderMap map[string]string) (out string) {
	var placeHolderConstMap = map[string]PlaceHolderFunc{
		"TODAY": func() string {
			return time.Now().Format("2 Jan 2006")
		},
	}

	templateWithFormatSpecifiers, placeHolders := convertToTemplateWithFormatSpecifiersAndPlaceHolders(template)
	args := make([]interface{}, 0, len(placeHolders))

	for _, placeHolder := range placeHolders {
		if placeHolderValue, ok := placeHolderMap[placeHolder]; ok {
			args = append(args, placeHolderValue)
			continue
		}

		if placeHolderValue, ok := placeHolderConstMap[placeHolder]; ok {
			args = append(args, placeHolderValue())
			continue
		}

		// ignore this placeholder if it doesn't exists in placeHolderMap (input from customer) and placeHolderConstMap (setting but code)
		args = append(args, fmt.Sprintf("{{%s}}", placeHolder))
	}

	return fmt.Sprintf(templateWithFormatSpecifiers, args...)
}
