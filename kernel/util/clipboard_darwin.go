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

//go:build darwin && !ios

// This file implements writing a list of file paths to macOS's NSPasteboard: it writes an array of NSURL via
// writeObjects: (NSPasteboardTypeFileURL / public.file-url), so apps like Finder can recognize and paste them as
// files.
//
// The logic follows Apple's official "Copying to a Pasteboard" three steps:
// 1) get the general pasteboard; 2) clearContents to clear it; 3) writeObjects: to write objects conforming to
// NSPasteboardWriting.
// NSURL is a built-in supported type; once a file URL is written, the system automatically provides
// representations like public.file-url, NSFilenamesPboardType, and public.utf8-plain-text, staying compatible
// with Finder and legacy APIs.
//
// Official docs and references:
//   - Pasteboard Programming Guide (macOS)
//     https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/PasteboardGuide106/Introduction/Introduction.html
//   - Copying to a Pasteboard (the three-step flow and writeObjects:)
//     https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/PasteboardGuide106/Articles/pbCopying.html
//   - NSPasteboard
//     https://developer.apple.com/documentation/appkit/nspasteboard
//   - NSPasteboardWriting (NSURL, NSString, etc. already implement it)
//     https://developer.apple.com/documentation/appkit/nspasteboardwriting
//
// The /* ... */ block below contains CGO-inlined Objective-C code, extracted and compiled by cgo -- it is not
// commented-out code.

package util

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

// writeFilePathsToPasteboard writes the path list to the general pasteboard, following the "Copying to a
// Pasteboard" three steps:
// 1) generalPasteboard; 2) clearContents; 3) writeObjects: passing in an NSURL array.
// NSURL conforms to NSPasteboardWriting; once written, the system automatically provides public.file-url,
// NSFilenamesPboardType, etc.
// paths is a UTF-8 path string array, count is its length.
static int writeFilePathsToPasteboard(const char** paths, int count) {
	if (count <= 0) return 0;
	NSMutableArray *arr = [NSMutableArray arrayWithCapacity:(NSUInteger)count];
	for (int i = 0; i < count; i++) {
		NSString *path = [NSString stringWithUTF8String:paths[i]];
		if (!path) continue;
		NSURL *url = [NSURL fileURLWithPath:path];
		if (url) [arr addObject:url];
	}
	// If there is no valid path at all (e.g. all invalid UTF-8 or unable to convert to NSURL), return -2 so the Go side can report an error
	if ([arr count] == 0) return -2;
	// Step 1: get the general pasteboard (used for cut/copy/paste)
	NSPasteboard *pb = [NSPasteboard generalPasteboard];
	// Step 2: clear existing content, then write only this batch of file paths
	[pb clearContents];
	// Step 3: writeObjects: requires objects conforming to NSPasteboardWriting, which NSURL already supports
	BOOL ok = [pb writeObjects:arr];
	return ok ? 0 : -1;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// WriteFilePaths writes a list of file paths to the system clipboard (general pasteboard), so they can be pasted
// as files in Finder and similar apps. See the Pasteboard Guide -- Copying to a Pasteboard for the implementation.
func WriteFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	// Allocate a C char* array to pass into Objective-C
	cPaths := make([]*C.char, len(paths))
	for i, p := range paths {
		cPaths[i] = C.CString(p)
	}
	defer func() {
		for _, c := range cPaths {
			C.free(unsafe.Pointer(c))
		}
	}()
	// Take the address of the first element to pass in as const char**
	ret := C.writeFilePathsToPasteboard((**C.char)(unsafe.Pointer(&cPaths[0])), C.int(len(paths)))
	switch ret {
	case 0:
		return nil
	case -2:
		return errors.New("no valid file paths to write (invalid UTF-8 or path)")
	default:
		return errors.New("failed to write file paths to pasteboard")
	}
}
