//go:build !unix

package session

import "os"

func fileOwner(os.FileInfo) (int, int) { return -1, -1 }
