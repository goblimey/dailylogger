package dailylogger

import (
	"os"
	"regexp"
	"testing"
	"time"

	ts "github.com/goblimey/go-tools/testsupport"
	"github.com/goblimey/portablesyscall"
)

// TestDailyLoggerIntegration is an integration test of the daily logger.  If
// it's run under a UNIX or Linux system, it must be run by root.
func TestDailyLoggerIntegration(t *testing.T) {

	// This test uses the filestore.  It creates a directory in /tmp containing
	// a plain file.  At the end it attempts to remove everything it created.
	//
	// It creates a production version of the daily logger.  It's expected to
	// produce one log file but it will start the log rollover goroutine.  If
	// it's run just before midnight that could result in two log files, which
	// would make the test fail.

	// user and goup are the user and group that the log file will be owned by.
	// Under Debian Linux, "www-data" always exists and is the user that runs
	// Apache.  The owner can only be changed if the test is run by root.
	const user = "www-data"
	const group = "www-data"

	directoryName, err := ts.CreateWorkingDirectory()
	if err != nil {
		t.Fatalf("createWorkingDirectory failed - %v", err)
	}
	defer ts.RemoveWorkingDirectory(directoryName)

	wantFilenamePattern := "daily.[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9].log"

	wantMessage := "hello world"

	buffer := []byte(wantMessage)

	now := time.Now()

	// Test.

	// Under Windows, the user and group are ignored.
	writer := New(now, ".", "", "", 0700, 0600, user, group)

	n, err := writer.Write(buffer)

	if err != nil {
		t.Fatalf("Write failed - %v", err)
	}

	if n != len(buffer) {
		t.Fatalf("Write returned %d - expected %d", n, len(buffer))
	}

	// Check.

	// Check that one log file was created and contains the expected contents.
	files, err := os.ReadDir(directoryName)
	if err != nil {
		t.Fatalf("error scanning directory %s - %s", directoryName, err.Error())
	}

	if len(files) != 1 {
		t.Fatalf("directory %s contains %d files.  Should contain exactly one.", directoryName, len(files))
	}

	match, err := regexp.MatchString(wantFilenamePattern, files[0].Name())
	if err != nil {
		t.Fatalf("error matching log file name %s - %s", files[0].Name(), err.Error())
	}
	if !match {
		t.Fatalf("directory %s contains file \"%s\", incorrect name format",
			directoryName, files[0].Name())
	}

	// Check the contents.
	inputFile, err := os.OpenFile(files[0].Name(), os.O_RDONLY, 0644)
	defer inputFile.Close()
	b := make([]byte, 8096)
	length, err := inputFile.Read(b)
	if err != nil {
		t.Fatalf("error reading logfile back - %v", err)
	}
	if length != len(buffer) {
		t.Fatalf("logfile %s contains %d bytes - expected %d",
			files[0].Name(), length, len(buffer))
	}

	contents := string(b[:length])

	if wantMessage != contents {
		t.Fatalf("logfile %s contains \"%s\" - expected \"%s\"",
			files[0].Name(), contents, wantMessage)
	}

	if portablesyscall.OSName != "windows" {
		// Except when running under Windows, the owner of the file should
		// be changed.  We must be running as root to do this.
		if os.Getuid() != 0 {
			t.Error("must be root to run this test")
			return
		}
	}
}
