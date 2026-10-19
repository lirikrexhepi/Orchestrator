//go:build windows

package servers

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listeningSockets enumerates TCP listeners via Get-NetTCPConnection, which
// reports the owning PID directly. netstat would need output parsing across
// locales; the cmdlet returns structured data.
func listeningSockets(ctx context.Context) ([]listener, error) {
	if out, err := nativeListeners(); err == nil {
		return out, nil
	}
	return shellListeners(ctx)
}

func processTable(ctx context.Context) (map[int]processInfo, error) {
	if table, err := nativeProcessTable(); err == nil {
		return table, nil
	}
	return shellProcessTable(ctx)
}

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const tcpTableOwnerPIDListener = 3

func extendedTCPTable(family uint32) ([]byte, error) {
	if err := procGetExtendedTcpTable.Find(); err != nil {
		return nil, err
	}
	size := uint32(64 * 1024)
	for attempt := 0; attempt < 4; attempt++ {
		buf := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			0,
			uintptr(family),
			tcpTableOwnerPIDListener,
			0)
		switch syscall.Errno(r) {
		case 0:
			return buf, nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			size += 4096
			continue
		default:
			return nil, syscall.Errno(r)
		}
	}
	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}

func nativeListeners() ([]listener, error) {
	var out []listener

	v4, err := extendedTCPTable(windows.AF_INET)
	if err != nil {
		return nil, err
	}
	const v4Row = 24
	count := int(binary.LittleEndian.Uint32(v4))
	for i := 0; i < count && 4+(i+1)*v4Row <= len(v4); i++ {
		row := v4[4+i*v4Row:]
		port := int(binary.BigEndian.Uint16(row[8:10]))
		pid := int(binary.LittleEndian.Uint32(row[20:24]))
		if pid <= 0 || port <= 0 {
			continue
		}
		out = append(out, listener{pid: pid, port: port, address: net.IP(row[4:8]).String()})
	}

	v6, err := extendedTCPTable(windows.AF_INET6)
	if err != nil {
		return nil, err
	}
	const v6Row = 56
	count = int(binary.LittleEndian.Uint32(v6))
	for i := 0; i < count && 4+(i+1)*v6Row <= len(v6); i++ {
		row := v6[4+i*v6Row:]
		port := int(binary.BigEndian.Uint16(row[20:22]))
		pid := int(binary.LittleEndian.Uint32(row[52:56]))
		if pid <= 0 || port <= 0 {
			continue
		}
		address := make(net.IP, net.IPv6len)
		copy(address, row[0:16])
		out = append(out, listener{pid: pid, port: port, address: address.String()})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].port < out[j].port })
	return out, nil
}

func nativeProcessTable() (map[int]processInfo, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}

	table := make(map[int]processInfo, 512)
	for {
		pid := int(entry.ProcessID)
		table[pid] = processInfo{
			pid:     pid,
			parent:  int(entry.ParentProcessID),
			name:    windows.UTF16ToString(entry.ExeFile[:]),
			command: commandLine(entry.ProcessID),
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return table, nil
}

func commandLine(pid uint32) string {
	if pid <= 4 {
		return ""
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(process)

	buf := make([]uint64, 512)
	for attempt := 0; attempt < 3; attempt++ {
		var needed uint32
		err := windows.NtQueryInformationProcess(process, windows.ProcessCommandLineInformation,
			unsafe.Pointer(&buf[0]), uint32(len(buf)*8), &needed)
		if err == nil {
			text := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
			if text.Length == 0 || text.Buffer == nil {
				return ""
			}
			return windows.UTF16ToString(unsafe.Slice(text.Buffer, text.Length/2))
		}
		if needed <= uint32(len(buf)*8) || needed > 1<<20 {
			return ""
		}
		buf = make([]uint64, (needed+7)/8)
	}
	return ""
}

func shellListeners(ctx context.Context) ([]listener, error) {
	const script = `Get-NetTCPConnection -State Listen |
		Select-Object LocalAddress,LocalPort,OwningProcess |
		ConvertTo-Json -Compress -Depth 2`

	raw, err := runPowerShell(ctx, script)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		LocalAddress  string `json:"LocalAddress"`
		LocalPort     int    `json:"LocalPort"`
		OwningProcess int    `json:"OwningProcess"`
	}
	if err := decodeRows(raw, &rows); err != nil {
		return nil, err
	}

	out := make([]listener, 0, len(rows))
	for _, row := range rows {
		if row.OwningProcess <= 0 || row.LocalPort <= 0 {
			continue
		}
		out = append(out, listener{
			pid:     row.OwningProcess,
			port:    row.LocalPort,
			address: row.LocalAddress,
		})
	}
	return out, nil
}

func shellProcessTable(ctx context.Context) (map[int]processInfo, error) {
	const script = `Get-CimInstance Win32_Process |
		Select-Object ProcessId,ParentProcessId,Name,CommandLine |
		ConvertTo-Json -Compress -Depth 2`

	raw, err := runPowerShell(ctx, script)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		ProcessId       int    `json:"ProcessId"`
		ParentProcessId int    `json:"ParentProcessId"`
		Name            string `json:"Name"`
		CommandLine     string `json:"CommandLine"`
	}
	if err := decodeRows(raw, &rows); err != nil {
		return nil, err
	}

	table := make(map[int]processInfo, len(rows))
	for _, row := range rows {
		table[row.ProcessId] = processInfo{
			pid:     row.ProcessId,
			parent:  row.ParentProcessId,
			name:    row.Name,
			command: row.CommandLine,
		}
	}
	return table, nil
}

// decodeRows tolerates PowerShell collapsing a single-element array into a bare
// object, which ConvertTo-Json does silently.
func decodeRows(raw []byte, target any) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "{") {
		trimmed = "[" + trimmed + "]"
	}
	return json.Unmarshal([]byte(trimmed), target)
}

func runPowerShell(ctx context.Context, script string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Output()
}

// stop ends a listener. Windows has no graceful signal, so the process tree is
// taken down together — a dev server that survived with orphaned workers still
// holding the port would defeat the point of the button.
func stop(ctx context.Context, pid int) error {
	cmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}
