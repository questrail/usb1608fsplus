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
	"runtime"
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

	// Parse the config flags to determine the config JSON filename
	var (
		configFlag = flag.String("config", "./config.json", "JSON config filename.")
	)
	flag.Parse()
	configFilename, err := homedir.Expand(*configFlag)
	if err != nil {
		log.Fatalln(err)
	}

	// If running from RPi, set GPIO3 low.
	if runtime.GOARCH == "arm" {
		gpio3, err := rpi.OpenPin(3, rpi.OUT)
		if err != nil {
			panic(err)
		}
		defer gpio3.Close()
		gpio3.Write(rpi.LOW)
	}

	// Initialize the USB Context
	ctx, err := libusb.Init()
	if err != nil {
		log.Fatal("Couldn't create USB context. Ending now.")
	}
	defer ctx.Exit()

	// Find the first USB fevice with the VendorID and ProductID matching the MCC
	// USB-1608FS-Plus DAQ
	daq, err := usb1608fsplus.GetFirstDevice(ctx)
	if err != nil {
		log.Fatalf("Couldn't find a USB-1608FS-Plus: %s", err)
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
	dec := json.NewDecoder(bytes.NewReader(configData))
	var configJSON = struct {
		OutputFile                 string `json:"output_file"`
		ScansPerBuffer             int    `json:"scans_per_buffer"`
		TotalBuffers               int    `json:"total_buffers"`
		*usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		"",
		0,
		0,
		ai,
	}
	if err := dec.Decode(&configJSON); err != nil {
		log.Fatalf("parse USB-1608FS-Plus: %v", err)
	}
	scansPerBuffer := configJSON.ScansPerBuffer
	totalBuffers := configJSON.TotalBuffers
	outputDir := configJSON.OutputFile
	ai.SetScanRanges()

	// Setup dir to hold output files.
	os.Mkdir(outputDir, 0755)
	baseFilename := path.Base(outputDir)

	// Read the scan ranges
	time.Sleep(millisecondDelay * time.Millisecond)
	_, err = ai.ScanRanges()

	// Read the totalScans using splitScansIn number of scans
	ai.StartScan(0)
	totalBytesRead := 0

	for j := 0; j < totalBuffers; j++ {
		// time.Sleep(millisecondDelay * time.Millisecond)
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
		filename := fmt.Sprintf("%s_%d.dat", baseFilename, j)
		path := path.Join(outputDir, filename)
		go ioutil.WriteFile(path, data, 0666)
	}

	// Stop the analog scan and close the DAQ
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
}
