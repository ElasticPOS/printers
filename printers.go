// Copyright 2013 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package printers Windows printing.
package printers

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:generate go run cmd/mksyscall/mksyscall_windows.go -output zapi.go printers.go

//sys	GetDefaultPrinter(buf *uint16, bufN *uint32) (err error) = winspool.GetDefaultPrinterW
//sys	SetDefaultPrinter(name *uint16) (err error) = winspool.SetDefaultPrinterW
//sys	ClosePrinter(h syscall.Handle) (err error) = winspool.ClosePrinter
//sys	OpenPrinter(name *uint16, h *syscall.Handle, defaults *PrinterDefaults) (err error) = winspool.OpenPrinterW
//sys	StartDocPrinter(h syscall.Handle, level uint32, docInfo *DOC_INFO_1) (err error) = winspool.StartDocPrinterW
//sys	EndDocPrinter(h syscall.Handle) (err error) = winspool.EndDocPrinter
//sys	WritePrinter(h syscall.Handle, buf *byte, bufN uint32, written *uint32) (err error) = winspool.WritePrinter
//sys	StartPagePrinter(h syscall.Handle) (err error) = winspool.StartPagePrinter
//sys	EndPagePrinter(h syscall.Handle) (err error) = winspool.EndPagePrinter
//sys	EnumPrinters(flags uint32, name *uint16, level uint32, buf *byte, bufN uint32, needed *uint32, returned *uint32) (err error) = winspool.EnumPrintersW
//sys	GetPrinterDriver(h syscall.Handle, env *uint16, level uint32, di *byte, n uint32, needed *uint32) (err error) = winspool.GetPrinterDriverW
//sys	EnumJobs(h syscall.Handle, firstJob uint32, noJobs uint32, level uint32, buf *byte, bufN uint32, bytesNeeded *uint32, jobsReturned *uint32) (err error) = winspool.EnumJobsW
//sys	DocumentProperties(hWnd uint32, h syscall.Handle, pDeviceName *uint16, devModeOut *DevMode, devModeIn *DevMode, fMode uint32) (err error) = winspool.DocumentPropertiesW
//sys	GetPrinter(h syscall.Handle, level uint32, buf *byte, bufN uint32, needed *uint32) (err error) = winspool.GetPrinterW
//sys	SetPrinter(h syscall.Handle, level uint32, buf *byte, command uint32) (err error) = winspool.SetPrinterW
//sys	AddForm(h syscall.Handle, level uint32, form *FORM_INFO_1) (err error) = winspool.AddFormW
//sys	DeleteForm(h syscall.Handle, pFormName *uint16) (err error) = winspool.DeleteFormW
//sys	EnumForms(h syscall.Handle, level uint32, pForm *byte, cbBuf uint32, pcbNeeded *uint32, pcReturned *uint32) (err error) = winspool.EnumFormsW

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type DOC_INFO_1 struct {
	/*
	  LPTSTR pDocName;
	  LPTSTR pOutputFile;
	  LPTSTR pDatatype;
	*/
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type FORM_INFO_1 struct {
	/*
	  DWORD  Flags;
	  LPTSTR pName;
	  SIZEL  Size;
	  RECTL  ImageableArea;
	*/
	Flags         uint32
	pName         *uint16
	Size          SIZE
	ImageableArea Rect
}

// SIZE windows.Coord
type SIZE struct {
	Width  uint32 // 宽度，以千毫米为单位
	Height uint32 // 高度，以千毫米为单位
}

// Rect windows.Rect
type Rect struct {
	Left   uint32
	Top    uint32
	Right  uint32
	Bottom uint32
}

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type PRINTER_INFO_9 struct {
	/*
	  LPDEVMODE pDevMode;
	*/
	pDevMode *DevMode
}

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type PRINTER_INFO_2 struct {
	/*
	  LPTSTR               pServerName;
	  LPTSTR               pPrinterName;
	  LPTSTR               pShareName;
	  LPTSTR               pPortName;
	  LPTSTR               pDriverName;
	  LPTSTR               pComment;
	  LPTSTR               pLocation;
	  LPDEVMODE            pDevMode;
	  LPTSTR               pSepFile;
	  LPTSTR               pPrintProcessor;
	  LPTSTR               pDatatype;
	  LPTSTR               pParameters;
	  PSECURITY_DESCRIPTOR pSecurityDescriptor;
	  DWORD                Attributes;
	  DWORD                Priority;
	  DWORD                DefaultPriority;
	  DWORD                StartTime;
	  DWORD                UntilTime;
	  DWORD                Status;
	  DWORD                cJobs;
	  DWORD                AveragePPM;
	*/
	pServerName         *uint16
	pPrinterName        *uint16
	pShareName          *uint16
	pPortName           *uint16
	pDriverName         *uint16
	pComment            *uint16
	pLocation           *uint16
	pDevMode            *DevMode
	pSepFile            *uint16
	pPrintProcessor     *uint16
	pDatatype           *uint16
	pParameters         *uint16
	pSecurityDescriptor uintptr
	attributes          uint32
	priority            uint32
	defaultPriority     uint32
	startTime           uint32
	untilTime           uint32
	status              uint32
	cJobs               uint32
	averagePPM          uint32
}

func (pi *PRINTER_INFO_2) GetDataType() string {
	return utf16PtrToString(pi.pDatatype)
}

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type PRINTER_INFO_5 struct {
	/*
	  LPTSTR pPrinterName;
	  LPTSTR pPortName;
	  DWORD  Attributes;
	  DWORD  DeviceNotSelectedTimeout;
	  DWORD  TransmissionRetryTimeout;
	*/
	PrinterName              *uint16
	PortName                 *uint16
	Attributes               uint32
	DeviceNotSelectedTimeout uint32
	TransmissionRetryTimeout uint32
}

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type DRIVER_INFO_8 struct {
	/*
	  DWORD     cVersion;
	  LPTSTR    pName;
	  LPTSTR    pEnvironment;
	  LPTSTR    pDriverPath;
	  LPTSTR    pDataFile;
	  LPTSTR    pConfigFile;
	  LPTSTR    pHelpFile;
	  LPTSTR    pDependentFiles;
	  LPTSTR    pMonitorName;
	  LPTSTR    pDefaultDataType;
	  LPTSTR    pszzPreviousNames;
	  FILETIME  ftDriverDate;
	  DWORDLONG dwlDriverVersion;
	  LPTSTR    pszMfgName;
	  LPTSTR    pszOEMUrl;
	  LPTSTR    pszHardwareID;
	  LPTSTR    pszProvider;
	  LPTSTR    pszPrintProcessor;
	  LPTSTR    pszVendorSetup;
	  LPTSTR    pszzColorProfiles;
	  LPTSTR    pszInfPath;
	  DWORD     dwPrinterDriverAttributes;
	  LPTSTR    pszzCoreDriverDependencies;
	  FILETIME  ftMinInboxDriverVerDate;
	  DWORDLONG dwlMinInboxDriverVerVersion;
	*/
	Version                  uint32
	Name                     *uint16
	Environment              *uint16
	DriverPath               *uint16
	DataFile                 *uint16
	ConfigFile               *uint16
	HelpFile                 *uint16
	DependentFiles           *uint16
	MonitorName              *uint16
	DefaultDataType          *uint16
	PreviousNames            *uint16
	DriverDate               windows.Filetime
	DriverVersion            uint64
	MfgName                  *uint16
	OEMUrl                   *uint16
	HardwareID               *uint16
	Provider                 *uint16
	PrintProcessor           *uint16
	VendorSetup              *uint16
	ColorProfiles            *uint16
	InfPath                  *uint16
	PrinterDriverAttributes  uint32
	CoreDriverDependencies   *uint16
	MinInboxDriverVerDate    windows.Filetime
	MinInboxDriverVerVersion uint32
}

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
type JOB_INFO_1 struct {
	/*
	  DWORD      JobId;
	  LPTSTR     pPrinterName;
	  LPTSTR     pMachineName;
	  LPTSTR     pUserName;
	  LPTSTR     pDocument;
	  LPTSTR     pDatatype;
	  LPTSTR     pStatus;
	  DWORD      Status;
	  DWORD      Priority;
	  DWORD      Position;
	  DWORD      TotalPages;
	  DWORD      PagesPrinted;
	  SYSTEMTIME Submitted;
	*/
	JobID        uint32
	PrinterName  *uint16
	MachineName  *uint16
	UserName     *uint16
	Document     *uint16
	DataType     *uint16
	Status       *uint16
	StatusCode   uint32
	Priority     uint32
	Position     uint32
	TotalPages   uint32
	PagesPrinted uint32
	Submitted    windows.Systemtime
}

//goland:noinspection GoSnakeCaseUsage
const (
	PRINTER_ENUM_LOCAL       = 2
	PRINTER_ENUM_CONNECTIONS = 4

	PRINTER_DRIVER_XPS = 0x00000002
)

//goland:noinspection GoSnakeCaseUsage,SpellCheckingInspection
const (
	JOB_STATUS_PAUSED            = 0x00000001 // Job is paused
	JOB_STATUS_ERROR             = 0x00000002 // An error is associated with the job
	JOB_STATUS_DELETING          = 0x00000004 // Job is being deleted
	JOB_STATUS_SPOOLING          = 0x00000008 // Job is spooling
	JOB_STATUS_PRINTING          = 0x00000010 // Job is printing
	JOB_STATUS_OFFLINE           = 0x00000020 // Printer is offline
	JOB_STATUS_PAPEROUT          = 0x00000040 // Printer is out of paper
	JOB_STATUS_PRINTED           = 0x00000080 // Job has printed
	JOB_STATUS_DELETED           = 0x00000100 // Job has been deleted
	JOB_STATUS_BLOCKED_DEVQ      = 0x00000200 // Printer driver cannot print the job
	JOB_STATUS_USER_INTERVENTION = 0x00000400 // User action required
	JOB_STATUS_RESTART           = 0x00000800 // Job has been restarted
	JOB_STATUS_COMPLETE          = 0x00001000 // Job has been delivered to the printer
	JOB_STATUS_RETAINED          = 0x00002000 // Job has been retained in the print queue
	JOB_STATUS_RENDERING_LOCALLY = 0x00004000 // Job rendering locally on the client
)

// GetDefault 获取默认打印机名称
func GetDefault() (printer string, err error) {
	b := make([]uint16, 3)
	n := uint32(len(b))
	err = GetDefaultPrinter(&b[0], &n)
	if err != nil {
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			return
		}
		b = make([]uint16, n)
		err = GetDefaultPrinter(&b[0], &n)
		if err != nil {
			return
		}
	}
	printer = windows.UTF16ToString(b)
	return
}

// SetDefault 根据打印机名称设置默认打印机
func SetDefault(printer string) (err error) {
	docName, _ := windows.UTF16FromString(printer)
	err = SetDefaultPrinter(&(docName)[0])
	return
}

// ReadNames return printer names on the system
func ReadNames() ([]string, error) {
	const flags = PRINTER_ENUM_LOCAL | PRINTER_ENUM_CONNECTIONS
	var needed, returned uint32
	buf := make([]byte, 1)
	err := EnumPrinters(flags, nil, 5, &buf[0], uint32(len(buf)), &needed, &returned)
	if err != nil {
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, err
		}
		buf = make([]byte, needed)
		err = EnumPrinters(flags, nil, 5, &buf[0], uint32(len(buf)), &needed, &returned)
		if err != nil {
			return nil, err
		}
	}
	ps := (*[1024]PRINTER_INFO_5)(unsafe.Pointer(&buf[0]))[:returned:returned]
	names := make([]string, 0, returned)
	for _, p := range ps {
		names = append(names, windows.UTF16PtrToString(p.PrinterName))
	}
	return names, nil
}

type Printer struct {
	h syscall.Handle
}

func Open(name string) (*Printer, error) {
	var p Printer
	docName, _ := windows.UTF16FromString(name)
	err := OpenPrinter(&(docName)[0], &p.h, nil)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func OpenWithDefaults(name string, defaults *PrinterDefaults) (*Printer, error) {
	var p Printer
	docName, _ := windows.UTF16FromString(name)
	err := OpenPrinter(&(docName)[0], &p.h, defaults)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

type PrinterDefaults struct {
	Datatype      *uint16
	pDevMode      *DevMode
	DesiredAccess uint32
}

// DriverInfo stores information about printer driver.
type DriverInfo struct {
	Name        string
	Environment string
	DriverPath  string
	Attributes  uint32
}

// JobInfo stores information about a print job.
type JobInfo struct {
	JobID           uint32
	UserMachineName string
	UserName        string
	DocumentName    string
	DataType        string
	Status          string
	StatusCode      uint32
	Priority        uint32
	Position        uint32
	TotalPages      uint32
	PagesPrinted    uint32
	Submitted       time.Time
}

// FormInfo stores information about a print form.
//
//goland:noinspection SpellCheckingInspection
type FormInfo struct {
	Flags         uint32
	Name          string
	Size          SIZE
	ImageableArea Rect
}

// Forms returns information about all paper size forms on the print server
func (p *Printer) Forms() (forms []FormInfo, err error) {
	var bytesNeeded, formsReturned uint32
	buf := make([]byte, 1)
	for {
		err = EnumForms(p.h, 1, &buf[0], uint32(len(buf)), &bytesNeeded, &formsReturned)
		if err == nil {
			break
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			return
		}
		if bytesNeeded <= uint32(len(buf)) {
			return
		}
		buf = make([]byte, bytesNeeded)
	}
	if formsReturned <= 0 {
		return
	}
	forms = make([]FormInfo, 0, formsReturned)
	formsInfo := (*[2048]FORM_INFO_1)(unsafe.Pointer(&buf[0]))[:formsReturned:formsReturned]
	for _, form := range formsInfo {
		formInfo := FormInfo{
			Flags:         form.Flags,
			Size:          form.Size,
			ImageableArea: form.ImageableArea,
		}
		if form.pName != nil {
			formInfo.Name = windows.UTF16PtrToString(form.pName)
		}
		forms = append(forms, formInfo)
	}
	return
}

// Jobs returns information about all print jobs on this printer
func (p *Printer) Jobs() ([]JobInfo, error) {
	var bytesNeeded, jobsReturned uint32
	buf := make([]byte, 1)
	for {
		err := EnumJobs(p.h, 0, 255, 1, &buf[0], uint32(len(buf)), &bytesNeeded, &jobsReturned)
		if err == nil {
			break
		}
		if err != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, err
		}
		if bytesNeeded <= uint32(len(buf)) {
			return nil, err
		}
		buf = make([]byte, bytesNeeded)
	}
	if jobsReturned <= 0 {
		return nil, nil
	}
	pjs := make([]JobInfo, 0, jobsReturned)
	ji := (*[2048]JOB_INFO_1)(unsafe.Pointer(&buf[0]))[:jobsReturned:jobsReturned]
	for _, j := range ji {
		pji := JobInfo{
			JobID:        j.JobID,
			StatusCode:   j.StatusCode,
			Priority:     j.Priority,
			Position:     j.Position,
			TotalPages:   j.TotalPages,
			PagesPrinted: j.PagesPrinted,
		}
		if j.MachineName != nil {
			pji.UserMachineName = windows.UTF16PtrToString(j.MachineName)
		}
		if j.UserName != nil {
			pji.UserName = windows.UTF16PtrToString(j.UserName)
		}
		if j.Document != nil {
			pji.DocumentName = windows.UTF16PtrToString(j.Document)
		}
		if j.DataType != nil {
			pji.DataType = windows.UTF16PtrToString(j.DataType)
		}
		if j.Status != nil {
			pji.Status = windows.UTF16PtrToString(j.Status)
		}
		if strings.TrimSpace(pji.Status) == "" {
			if pji.StatusCode == 0 {
				pji.Status += "Queue Paused, "
			}
			if pji.StatusCode&JOB_STATUS_PRINTING != 0 {
				pji.Status += "Printing, "
			}
			if pji.StatusCode&JOB_STATUS_PAUSED != 0 {
				pji.Status += "Paused, "
			}
			if pji.StatusCode&JOB_STATUS_ERROR != 0 {
				pji.Status += "Error, "
			}
			if pji.StatusCode&JOB_STATUS_DELETING != 0 {
				pji.Status += "Deleting, "
			}
			if pji.StatusCode&JOB_STATUS_SPOOLING != 0 {
				pji.Status += "Spooling, "
			}
			if pji.StatusCode&JOB_STATUS_OFFLINE != 0 {
				pji.Status += "Printer Offline, "
			}
			if pji.StatusCode&JOB_STATUS_PAPEROUT != 0 {
				pji.Status += "Out of Paper, "
			}
			if pji.StatusCode&JOB_STATUS_PRINTED != 0 {
				pji.Status += "Printed, "
			}
			if pji.StatusCode&JOB_STATUS_DELETED != 0 {
				pji.Status += "Deleted, "
			}
			if pji.StatusCode&JOB_STATUS_BLOCKED_DEVQ != 0 {
				pji.Status += "Driver Error, "
			}
			if pji.StatusCode&JOB_STATUS_USER_INTERVENTION != 0 {
				pji.Status += "User Action Required, "
			}
			if pji.StatusCode&JOB_STATUS_RESTART != 0 {
				pji.Status += "Restarted, "
			}
			if pji.StatusCode&JOB_STATUS_COMPLETE != 0 {
				pji.Status += "Sent to Printer, "
			}
			if pji.StatusCode&JOB_STATUS_RETAINED != 0 {
				pji.Status += "Retained, "
			}
			if pji.StatusCode&JOB_STATUS_RENDERING_LOCALLY != 0 {
				pji.Status += "Rendering on Client, "
			}
			pji.Status = strings.TrimRight(pji.Status, ", ")
		}
		pji.Submitted = time.Date(
			int(j.Submitted.Year),
			time.Month(int(j.Submitted.Month)),
			int(j.Submitted.Day),
			int(j.Submitted.Hour),
			int(j.Submitted.Minute),
			int(j.Submitted.Second),
			int(1000*j.Submitted.Milliseconds),
			time.Local,
		).UTC()
		pjs = append(pjs, pji)
	}
	return pjs, nil
}

// DriverInfo returns information about printer p driver.
func (p *Printer) DriverInfo() (*DriverInfo, error) {
	var needed uint32
	err := GetPrinterDriver(p.h, nil, 8, nil, needed, &needed)
	if err != windows.ERROR_INSUFFICIENT_BUFFER {
		return nil, err
	}

	b := make([]byte, needed)
	err = GetPrinterDriver(p.h, nil, 8, &b[0], needed, &needed)
	if err != nil {
		return nil, err
	}

	di := (*DRIVER_INFO_8)(unsafe.Pointer(&b[0]))
	return &DriverInfo{
		Attributes:  di.PrinterDriverAttributes,
		Name:        windows.UTF16PtrToString(di.Name),
		DriverPath:  windows.UTF16PtrToString(di.DriverPath),
		Environment: windows.UTF16PtrToString(di.Environment),
	}, nil
}

// GetPrinter2 get Printer Info 2
func (p *Printer) GetPrinter2() (printerInfo *PRINTER_INFO_2, err error) {
	var needed uint32
	var buf = make([]byte, 1)

	var r1 uintptr
	r1, _, err = procGetPrinterW.Call(uintptr(p.h), 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&needed)))
	if r1 == 0 {
		var newBuf = make([]byte, int(needed))
		var newLen = uint32(len(newBuf))
		err = GetPrinter(p.h, 2, &newBuf[0], newLen, &needed)
		if err != nil {
			//fmt.Println("Failed: ", err)
			return
		}
		printerInfo = (*PRINTER_INFO_2)(unsafe.Pointer(&newBuf[0]))
		//fmt.Println("Get Printer Info 2 Duplex Setting: ", printerInfo.pDevMode.dmDuplex)
	}
	return
}

func (p *Printer) SetPrinter2(printerInfo *PRINTER_INFO_2) (err error) {
	bs := (*[unsafe.Sizeof(printerInfo)]byte)(unsafe.Pointer(printerInfo))

	//fmt.Println("Set printer to duplex with the info 2...")
	err = SetPrinter(p.h, 2, &bs[0], 0)
	return
}

// GetPrinter9 get Printer Info 9
func (p *Printer) GetPrinter9() (printerInfo *PRINTER_INFO_9, err error) {
	var needed uint32
	var buf = make([]byte, 1)

	err = GetPrinter(p.h, 9, &buf[0], uint32(len(buf)), &needed)
	if err != nil {
		var newBuf = make([]byte, int(needed))
		err = GetPrinter(p.h, 9, &newBuf[0], uint32(len(newBuf)), &needed)
		if err != nil {
			//fmt.Println("Failed: ", err)
			return
		}
		printerInfo = (*PRINTER_INFO_9)(unsafe.Pointer(&newBuf[0]))
		//fmt.Println("Get Printer Info 9 Duplex Setting: ", printerInfo.pDevMode.dmDuplex)
	}
	return
}

func (p *Printer) SetPrinter9(printerInfo *PRINTER_INFO_9) (err error) {
	bs := (*[unsafe.Sizeof(printerInfo)]byte)(unsafe.Pointer(printerInfo))

	//fmt.Println("Set printer to duplex with the info 9...")
	err = SetPrinter(p.h, 9, &bs[0], 0)
	return
}

func (p *Printer) DocumentPropertiesGet(deviceName string) (devMode *DevMode, err error) {
	var pDeviceName *uint16
	if pDeviceName, err = windows.UTF16PtrFromString(deviceName); err != nil {
		return
	}

	var r1 uintptr
	r1, _, err = procDocumentPropertiesW.Call(0, uintptr(p.h), uintptr(unsafe.Pointer(pDeviceName)), 0, 0, 0)
	iDevModeSize := int32(r1)
	if iDevModeSize < 0 {
		return
	}

	devMode = new(DevMode)
	//devMode.dmSize = uint16(iDevModeSize)
	//devMode.dmSpecVersion = DM_SPECVERSION
	err = DocumentProperties(0, p.h, pDeviceName, devMode, new(DevMode), DM_COPY)

	//fmt.Println("From get:", devMode.dmDuplex)
	return
}

func (p *Printer) DocumentPropertiesSet(deviceName string, devMode *DevMode) (err error) {
	var pDeviceName *uint16
	pDeviceName, err = windows.UTF16PtrFromString(deviceName)
	if err != nil {
		return
	}

	err = DocumentProperties(0, p.h, pDeviceName, devMode, devMode, DM_MODIFY)
	return
}

func (p *Printer) GetDataType() (dataType string, err error) {
	var ptr2 *PRINTER_INFO_2
	if ptr2, err = p.GetPrinter2(); err != nil {
		return
	}
	dataType = ptr2.GetDataType()
	return
}

func (p *Printer) StartDocument(name, datatype string) error {
	docName, _ := windows.UTF16FromString(name)
	dataType, _ := windows.UTF16FromString(datatype)
	d := DOC_INFO_1{
		DocName:    &(docName)[0],
		OutputFile: nil,
		Datatype:   &(dataType)[0],
	}
	return StartDocPrinter(p.h, 1, &d)
}

// StartRawDocument calls StartDocument and passes either "RAW" or "XPS_PASS"
// as a document type, depending on if printer driver is XPS-based or not.
func (p *Printer) StartRawDocument(name string) error {
	di, err := p.DriverInfo()
	if err != nil {
		return err
	}
	// See https://support.microsoft.com/en-us/help/2779300/v4-print-drivers-using-raw-mode-to-send-pcl-postscript-directly-to-the
	// for details.
	datatype := "RAW"
	if di.Attributes&PRINTER_DRIVER_XPS != 0 {
		datatype = "XPS_PASS"
	}
	return p.StartDocument(name, datatype)
}

func (p *Printer) Write(b []byte) (int, error) {
	var written uint32
	err := WritePrinter(p.h, &b[0], uint32(len(b)), &written)
	if err != nil {
		return 0, err
	}
	return int(written), nil
}

func (p *Printer) EndDocument() error {
	return EndDocPrinter(p.h)
}

func (p *Printer) StartPage() error {
	return StartPagePrinter(p.h)
}

func (p *Printer) EndPage() error {
	return EndPagePrinter(p.h)
}

func (p *Printer) Close() error {
	return ClosePrinter(p.h)
}
