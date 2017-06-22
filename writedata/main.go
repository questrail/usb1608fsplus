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
	"os/exec"
	"path"
	"time"

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
	type RPi struct {
		GPIO   int    `json:"gpio"`
		Output string `json:"output"`
	}
	type AppConfig struct {
		SN           string `json:"daq_sn"`
		DisableGPIO3 bool   `json:"disable_gpio3"`
		OutputFile   string `json:"output_file"`
		RPi          []RPi  `json:"rpi"`
	}
	var appConfig AppConfig
	if err := dec.Decode(&appConfig); err != nil {
		log.Fatalf("Couldn't parse app config: %v", err)
	}
	outputDir := appConfig.OutputFile

	// Set RPi, GPIO outputs as desired.
	var pins = make([]*rpi.Pin, len(appConfig.RPi))
	for i, rpiGPIO := range appConfig.RPi {
		pins[i], err = rpi.OpenPin(rpiGPIO.GPIO, rpi.OUT)
		if err != nil {
			panic(err)
		}
		defer pins[i].Close()
		if rpiGPIO.Output == "low" {
			pins[i].Write(rpi.LOW)
		} else if rpiGPIO.Output == "high" {
			pins[i].Write(rpi.HIGH)
		}
	}

	/***********************************
	* Start talking to the MCC via USB *
	***********************************/

	// Initialize the USB Context
	ctx, err := usb1608fsplus.Init()
	if err != nil {
		log.Fatal("Couldn't create USB context. Ending now.")
	}
	defer ctx.Exit()

	// Create the USB-1608FS-Plus DAQ device using the given S/N
	daq, err := usb1608fsplus.GetFirstDevice(ctx)
	if err != nil {
		log.Fatalf("Couldn't get first device %s: %s", appConfig.SN, err)
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
		BuffersPerFile             int `json:"buffers_per_file"`
		NumFiles                   int `json:"num_files"`
		*usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		0,
		0,
		0,
		ai,
	}
	if err := dec.Decode(&configJSON); err != nil {
		log.Fatalf("parse USB-1608FS-Plus: %v", err)
	}
	scansPerBuffer := configJSON.ScansPerBuffer
	buffersPerFile := configJSON.BuffersPerFile
	numFiles := configJSON.NumFiles
	ai.SetScanRanges()

	var headerJSON = struct {
		OutputFile                string    `json:"output_file"`
		ScansPerBuffer            int       `json:"scans_per_buffer"`
		BuffersPerFile            int       `json:"buffers_per_file"`
		NumFiles                  int       `json:"num_files"`
		FileNum                   int       `json:"file_num"`
		SystemTime                time.Time `json:"system_time"`
		RTCTime                   string    `json:"rtc_time"`
		usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		"",
		scansPerBuffer,
		buffersPerFile,
		numFiles,
		0,
		time.Now(),
		"",
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

	bytesPerWord := 2
	expectedBytesPerFile := ai.NumEnabledChannels() * bytesPerWord * scansPerBuffer * buffersPerFile

	c := make(chan string)

	for fileNum := 0; fileNum < numFiles; fileNum++ {
		dataForFile := make([]byte, 0, expectedBytesPerFile)
		headerJSON.FileNum = fileNum
		go getRTCTime(c)
		headerJSON.SystemTime = time.Now()
		for bufferNum := 0; bufferNum < buffersPerFile; bufferNum++ {
			data, err := ai.ReadScan(scansPerBuffer)
			totalBytesRead += len(data)
			if err != nil {
				// Stop the analog scan and close the DAQ
				ai.StopScan()
				time.Sleep(millisecondDelay * time.Millisecond)
				daq.Close()
				log.Fatalf("Error reading scan: %s", err)
			}
			// Data is good so append
			dataForFile = append(dataForFile, data...)
		}
		headerJSON.RTCTime = <-c
		// Write the data to the output
		headerData, err := json.MarshalIndent(&headerJSON, "", "  ")
		if err != nil {
			headerData = []byte("Bad header")
		}
		headerFilename := fmt.Sprintf("%s_%d.hdr", baseFilename, fileNum)
		headerPath := path.Join(outputDir, headerFilename)
		go ioutil.WriteFile(headerPath, headerData, 0666)
		binaryFilename := fmt.Sprintf("%s_%d.dat", baseFilename, fileNum)
		binaryPath := path.Join(outputDir, binaryFilename)
		log.Printf("Writing %s", binaryFilename)
		go ioutil.WriteFile(binaryPath, dataForFile, 0666)
	}

	// Stop the analog scan and close the DAQ
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
}

func getRTCTime(c chan string) {
	var cmdOut []byte
	var err error
	if cmdOut, err = exec.Command("hwclock", "-r").Output(); err != nil {
		c <- "bad hwclock call"
	}
	c <- string(cmdOut)
}
