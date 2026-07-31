// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

//go:build windows

// This file implements the Windows Shell clipboard format CF_HDROP, used to transfer a set of existing file paths
// via the clipboard so that Explorer and similar apps can recognize and paste them as files.
//
// Reference docs:
//   - Shell clipboard and CF_HDROP: https://learn.microsoft.com/en-us/windows/win32/shell/clipboard
//   - DROPFILES structure: https://learn.microsoft.com/en-us/windows/win32/api/shlobj_core/ns-shlobj_core-dropfiles
//   - SetClipboardData (hMem must be GMEM_MOVEABLE, and "memory must be unlocked before the Clipboard is closed"):
//     https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
//   - Official sample "Copy information to the clipboard" (GlobalUnlock then SetClipboardData):
//     https://learn.microsoft.com/en-us/windows/win32/dataxchg/using-the-clipboard
//
// CF_HDROP is a predefined format, so RegisterClipboardFormat is not needed. The data is a global memory object
// (hGlobal) whose contents are a DROPFILES structure followed by a double-null-terminated array of path strings.

package util

import (
	"encoding/binary"
	"errors"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/gonutz/w32/v2"
)

const (
	// cfHDROP is the CF_HDROP clipboard format (predefined value 15), used to transfer the locations of a set of
	// existing files.
	// See Standard Clipboard Formats: https://learn.microsoft.com/en-us/windows/win32/dataxchg/standard-clipboard-formats
	cfHDROP       = 15
	dropfilesSize = 20 // Size of the DROPFILES struct (pFiles 4 + pt 8 + fNC 4 + fWide 4), https://learn.microsoft.com/en-us/windows/win32/api/shlobj_core/ns-shlobj_core-dropfiles
)

// WriteFilePaths writes a list of file paths to the system clipboard, so they can be pasted as files in Explorer.
//
// Per the documentation, CF_HDROP data is global memory pointed to by STGMEDIUM's hGlobal, with the memory
// contents being a DROPFILES structure.
// The clipboard API requires OpenClipboard, writing, and CloseClipboard to happen on the same thread, hence
// LockOSThread is needed.
// Call order: prepare the data first (GlobalAlloc -> GlobalLock -> write -> GlobalUnlock), then OpenClipboard ->
// EmptyClipboard -> SetClipboardData -> CloseClipboard.
// Unlike the official "Copy information to the clipboard" sample, memory preparation is moved ahead of
// OpenClipboard here, to shorten the time the clipboard is held.
func WriteFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	data, err := buildDropfilesData(paths)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	// Global memory object; the SetClipboardData docs require hMem to be allocated via GlobalAlloc(GMEM_MOVEABLE)
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
	size := uint32(len(data))
	hMem := w32.GlobalAlloc(w32.GMEM_MOVEABLE, size)
	if hMem == 0 {
		return syscall.Errno(w32.GetLastError())
	}

	ptr := w32.GlobalLock(hMem)
	if ptr == nil {
		w32.GlobalFree(hMem)
		return syscall.Errno(w32.GetLastError())
	}

	w32.MoveMemory(ptr, unsafe.Pointer(&data[0]), size)
	// Must Unlock before SetClipboardData, otherwise the system cannot properly manage the handle it takes over.
	// Docs: "The memory must be unlocked before the Clipboard is closed."
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
	w32.GlobalUnlock(hMem)

	if err := waitOpenClipboard(); err != nil {
		w32.GlobalFree(hMem)
		return err
	}
	defer w32.CloseClipboard()

	if !w32.EmptyClipboard() {
		w32.GlobalFree(hMem)
		return syscall.Errno(w32.GetLastError())
	}
	if w32.SetClipboardData(cfHDROP, w32.HANDLE(hMem)) == 0 {
		w32.GlobalFree(hMem)
		return syscall.Errno(w32.GetLastError())
	}
	// On success the system takes ownership of hMem, and the app must not write to or free it again; on failure the branch above already called GlobalFree.
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
	return nil
}

// buildDropfilesData builds a byte slice in CF_HDROP format.
//
// The format follows DROPFILES: pFiles is an offset pointing to a double-null-terminated array of path strings.
// https://learn.microsoft.com/en-us/windows/win32/api/shlobj_core/ns-shlobj_core-dropfiles
// The array consists of a number of "full path + terminating NULL" entries, followed by one more NULL to end the
// whole list.
// For example, with two files: c:\temp1.txt\0 c:\temp2.txt\0 \0
// Unicode is used here (fWide=1), so paths are UTF-16, each path includes a terminating null, followed by 2 more
// bytes of null at the end.
func buildDropfilesData(paths []string) ([]byte, error) {
	var totalLen = dropfilesSize
	for _, p := range paths {
		u16, err := syscall.UTF16FromString(p)
		if err != nil {
			return nil, err
		}
		totalLen += len(u16) * 2
	}
	totalLen += 2 // The null at the end of the array (the last one of the double-null terminator)

	buf := make([]byte, totalLen)
	// DROPFILES: pFiles=20 (offset of the path array relative to the start of this struct), pt=0,0, fNC=0, fWide=1 (Unicode)
	binary.LittleEndian.PutUint32(buf[0:4], 20)
	// pt.x, pt.y, fNC, fWide
	binary.LittleEndian.PutUint32(buf[16:20], 1)

	offset := dropfilesSize
	for _, p := range paths {
		u16, err := syscall.UTF16FromString(p)
		if err != nil {
			return nil, err
		}
		for _, c := range u16 {
			binary.LittleEndian.PutUint16(buf[offset:offset+2], c)
			offset += 2
		}
	}
	return buf, nil
}

// waitOpenClipboard retries opening the clipboard within a time limit.
// Only one process can hold the clipboard at a time (i.e. have OpenClipboard succeed).
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-openclipboard
func waitOpenClipboard() error {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if w32.OpenClipboard(0) {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return errors.New("open clipboard timeout")
}
