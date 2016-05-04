// Copyright (c) 2016 The mccdaq developers. All rights reserved.
// Project site: https://github.com/gotmc/mccdaq
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"time"

	"github.com/gotmc/libusb"
	"github.com/gotmc/mccdaq/usb1608fsplus"
	"github.com/mitchellh/go-homedir"
	rpi "github.com/nathan-osman/go-rpigpio"
)

const (
	millisecondDelay = 100
	termWidth        = 70
)

func main() {

	/****************************
	* Configure the application *
	****************************/

	// Parse the config flags to determine the config JSON filename
	var (
		configFlag = flag.String("config", "./remote_config.json", "JSON config filename.")
	)
	flag.Parse()
	configFilename, err := homedir.Expand(*configFlag)
	if err != nil {
		log.Fatalln(err)
	}

	// Setup the application config
	appConfigData, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Fatalf("Error reading the USB-1608FS-Plus JSON config file")
	}
	dec := json.NewDecoder(bytes.NewReader(appConfigData))
	var appConfig = struct {
		SN           string `json:"daq_sn"`
		DisableGPIO3 bool   `json:"disable_gpio3"`
		OutputFile   string `json:"output_file"`
	}{
		"",
		false,
		"",
	}
	if err := dec.Decode(&appConfig); err != nil {
		log.Fatalf("Couldn't parse app config: %v", err)
	}
	outputDir := appConfig.OutputFile

	// If desired, pull GPIO3 low
	if appConfig.DisableGPIO3 {
		log.Printf("Pulling GPIO3 low.")
		gpio3, err := rpi.OpenPin(3, rpi.OUT)
		if err != nil {
			panic(err)
		}
		defer gpio3.Close()
		gpio3.Write(rpi.LOW)
	}

	/***********************************
	* Start talking to the MCC via USB *
	***********************************/

	// Initialize the USB Context
	ctx, err := libusb.Init()
	if err != nil {
		log.Fatal("Couldn't create USB context. Ending now.")
	}
	defer ctx.Exit()

	// Create the USB-1608FS-Plus DAQ device using the given S/N
	daq, err := usb1608fsplus.NewViaSN(ctx, appConfig.SN)
	if err != nil {
		log.Fatalf("Something bad getting S/N happened: %s", err)
	}
	defer daq.Close()

	// Create new analog input and ensure the scan is stopped and buffer cleared
	ai, err := daq.NewAnalogInput()
	if err != nil {
		log.Fatalf("Error creating new analog input: %s", err)
	}
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.ClearScanBuffer()

	/**************************
	* Start the Analog Scan   *
	**************************/

	// Setup the analog input scan
	configData, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Fatalf("Error reading the USB-1608FS-Plus JSON config file")
	}
	dec = json.NewDecoder(bytes.NewReader(configData))
	var configJSON = struct {
		ScansPerBuffer             int `json:"scans_per_buffer"`
		TotalBuffers               int `json:"total_buffers"`
		*usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		0,
		0,
		ai,
	}
	if err := dec.Decode(&configJSON); err != nil {
		log.Fatalf("parse USB-1608FS-Plus: %v", err)
	}
	scansPerBuffer := configJSON.ScansPerBuffer
	totalBuffers := configJSON.TotalBuffers
	ai.SetScanRanges()

	var headerJSON = struct {
		OutputFile                string    `json:"output_file"`
		ScansPerBuffer            int       `json:"scans_per_buffer"`
		TotalBuffers              int       `json:"total_buffers"`
		Buffer                    int       `json:"buffer"`
		Timestamp                 time.Time `json:"timestamp"`
		usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		"",
		scansPerBuffer,
		totalBuffers,
		0,
		time.Now(),
		*ai,
	}

	// Setup dir to hold output files.
	err = os.MkdirAll(outputDir, 0755)
	if err != nil {
		log.Fatalf("Could not create output dir: %s", err)
	}
	baseFilename := path.Base(outputDir)
	headerJSON.OutputFile = baseFilename

	// Read the scan ranges
	time.Sleep(millisecondDelay * time.Millisecond)
	_, err = ai.ScanRanges()

	// Read the totalScans using splitScansIn number of scans
	ai.StartScan(0)
	totalBytesRead := 0

	for j := 0; j < totalBuffers; j++ {
		headerJSON.Buffer = j
		headerJSON.Timestamp = time.Now()
		data, err := ai.ReadScan(scansPerBuffer)
		totalBytesRead += len(data)
		if err != nil {
			// Stop the analog scan and close the DAQ
			ai.StopScan()
			time.Sleep(millisecondDelay * time.Millisecond)
			daq.Close()
			log.Fatalf("Error reading scan: %s", err)
		}
		// Write the data to the output
		headerData, err := json.MarshalIndent(&headerJSON, "", "  ")
		headerFilename := fmt.Sprintf("%s_%d.hdr", baseFilename, j)
		headerPath := path.Join(outputDir, headerFilename)
		go ioutil.WriteFile(headerPath, headerData, 0666)
		binaryFilename := fmt.Sprintf("%s_%d.dat", baseFilename, j)
		binaryPath := path.Join(outputDir, binaryFilename)
		log.Printf("Writing %s", binaryFilename)
		go ioutil.WriteFile(binaryPath, data, 0666)
	}

	// Stop the analog scan and close the DAQ
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
}
