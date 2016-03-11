// Copyright (c) 2016 The usb1608fsplus developers. All rights reserved.
// Project site: https://github.com/questrail/usb1608fsplus
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gotmc/libusb"
	"github.com/gotmc/mccdaq/usb1608fsplus"
)

func main() {
	ctx, err := libusb.Init()
	if err != nil {
		log.Fatal("Couldn't create USB context. Ending now.")
	}
	defer ctx.Exit()

	// Create the USB-1608FS-Plus DAQ device
	daq, err := usb1608fsplus.GetFromSN(ctx, "01ACD31D")
	if err != nil {
		log.Fatalf("Something bad getting S/N happened: %s", err)
	}
	// If you just want to grab the first USB-1608FS-Plus that's attached, you
	// can use:
	// daq, err := usb1608fsplus.GetFirstDevice(ctx)

	// Print some info about the device
	log.Printf("Vendor ID = 0x%x / Product ID = 0x%x\n", daq.DeviceDescriptor.VendorID,
		daq.DeviceDescriptor.ProductID)
	serialNumber, err := daq.SerialNumber()
	log.Printf("Serial number via control transfer = %s", serialNumber)
	log.Printf("USB ConfigurationIndex = %d\n", daq.ConfigDescriptor.ConfigurationIndex)
	log.Printf("Bulk endpoint address = 0x%x (%b)\n",
		daq.BulkEndpoint.EndpointAddress, daq.BulkEndpoint.EndpointAddress)

	// Test blinking the LED
	blinks := 5
	count, err := daq.BlinkLED(blinks)
	if err != nil {
		fmt.Errorf("Error blinking LED %s", err)
	}
	log.Printf("Sent %d byte of data to blink LED %d times.", count, blinks)

	// Get status
	status, err := daq.Status()
	log.Printf("Status = %v", status)

	// Read the calibration memory to setup the gain table
	gainTable, _ := daq.BuildGainTable()
	log.Printf("Slope = %v\n", gainTable.Slope)
	log.Printf("Intercept = %v\n", gainTable.Intercept)

	// Read one analog reading.
	daq.StopAnalogScan()
	time.Sleep(time.Second)
	foo, err := daq.ReadAnalogInput(1, 0)
	if err != nil {
		log.Fatalf("Error reading one analog input. %s", err)
	}
	log.Printf("Read analog input %d", foo)

	/**************************
	* Start the Analog Scan   *
	**************************/

	// Setup stuff
	var ranges = make([]byte, 8) // Range 0 is ±10V
	for i := 0; i < len(ranges); i++ {
		ranges[i] = 0
	}
	count = 256
	var channels byte = 0x01 // one bit for each channel
	var frequency float64 = 20000.0
	// options := byte(0x1 | 0x2) // immediate w/ internal pacer on
	options := byte(0x0 | 0x2 | 0x20) // bulk w/ internal pacer on
	numChannels := 1

	// Stop, clear, and configure
	daq.StopAnalogScan()
	time.Sleep(time.Second)
	daq.ClearScanBuffer()
	daq.ConfigAnalogScan(ranges)
	time.Sleep(2 * time.Second)
	blah, err := daq.ReadScanRanges()
	log.Printf("Ranges = %v\n", blah)

	// Start the scan
	daq.StartAnalogScan(count, frequency, channels, options)
	time.Sleep(1 * time.Second)
	data, err := daq.ReadScan(count, numChannels, options)
	for i := 0; i < 8; i += 2 {
		log.Printf("data[%d:%d] = %d %d\n", i, i+1, data[i+1], data[i])
	}
	for i := count - 8; i < count; i += 2 {
		log.Printf("data[%d:%d] = %d %d\n", i, i+1, data[i+1], data[i])
	}
	log.Printf("data is %d bytes\n", len(data))
	daq.StopAnalogScan()
	time.Sleep(1 * time.Second)

	daq.Close()

}
