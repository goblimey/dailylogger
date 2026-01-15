package dailylogger

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	ps "github.com/goblimey/portablesyscall"
	"github.com/google/uuid"
)

// TestGetDurationToJustAfterMidnight tests the getDurationToJustAfterMidnight method.
func TestGetDurationToJustAfterMidnight(t *testing.T) {
	locationUTC, _ := time.LoadLocation("UTC")
	start := time.Date(2020, time.February, 14, 22, 59, 0, 0, locationUTC)
	wantDuration1 := time.Hour + time.Minute + extraDuration

	got1 := getDurationToJustAfterMidnight(start)
	if got1.Nanoseconds() != int64(wantDuration1) {
		t.Errorf("wnt duration to be \"%d\" got \"%d\"", wantDuration1, got1.Nanoseconds())
		return
	}

	start = time.Date(2020, time.February, 14, 0, 30, 3, 4, locationUTC)
	wantDuration2 := 23*time.Hour + 30*time.Minute + extraDuration - (3*time.Second + 4*time.Nanosecond)
	got2 := getDurationToJustAfterMidnight(start)
	if got2.Nanoseconds() != int64(wantDuration2) {
		t.Errorf("want duration to be \"%d\" got \"%d\"", wantDuration2, got2.Nanoseconds())
		return
	}

	// Test using a time that's not in UTC - in July Paris is two
	// hours ahead of UTC.
	locationParis, _ := time.LoadLocation("Europe/Paris")
	start = time.Date(2020, time.July, 4, 12, 5, 0, 0, locationParis)
	wantDuration3 := 11*time.Hour + 55*time.Minute + extraDuration
	got3 := getDurationToJustAfterMidnight(start)
	if got3.Nanoseconds() != int64(wantDuration3) {
		t.Errorf("want duration to be \"%d\" actually \"%d\"", wantDuration3, got3.Nanoseconds())
		return
	}
}

// TestLogging tests that logging works - creates a file of the right name with the
// right contents.
func TestLogging(t *testing.T) {

	// This test uses the filestore.  It creates a temporary directory containing
	// a plain file.  At the end it attempts to remove everything it created.

	testDirectoryName, err := CreateWorkingDirectory()
	if err != nil {
		t.Errorf("createWorkingDirectory failed - %v", err)
		return
	}
	defer RemoveWorkingDirectory(testDirectoryName)

	// Under a POSIX system, must be root to run this test.
	if ps.OSName != "windows" && syscall.Getuid() != 0 {
		// Not root - fail.
		t.Error("must be root to run this test")
		return
	}

	const wantLogDirBaseName = "dir"
	const logDirPathName = "./" + wantLogDirBaseName
	const leader = "foo."
	const trailer = ".bar"
	const wantLogFileName = "foo.2020-02-14.bar"
	const logFilePathName = wantLogDirBaseName + "/" + wantLogFileName
	const userName = "bin"
	const group = "daemon"
	const wantContents = "hello world"
	const wantDirPermissions os.FileMode = 0700
	const wantFilePermissions os.FileMode = 0600
	buffer := []byte(wantContents)

	locationParis, _ := time.LoadLocation("Europe/Paris")
	now := time.Date(2020, time.February, 14, 1, 2, 3, 4, locationParis)

	// Test.
	writer := New(now, logDirPathName, leader, trailer, userName, group, wantDirPermissions, wantFilePermissions)

	n, err := writer.Write(buffer)
	if err != nil {
		t.Errorf("Write failed - %v", err)
		return
	}

	// Check.

	// Check the return from write.
	if n != len(buffer) {
		t.Errorf("Write returned %d - want %d", n, len(buffer))
	}

	// Check that the test directory contains one file, a directory with the given name..
	filesInTestDirectory, tde := os.ReadDir(testDirectoryName)
	if tde != nil {
		t.Errorf("error scanning directory %s - %v", testDirectoryName, tde)
		return
	}

	if len(filesInTestDirectory) != 1 {
		t.Errorf("want 1 file got %d", len(filesInTestDirectory))
		return
	}

	// Check that the file is a directory
	dirInfo := filesInTestDirectory[0]
	if !dirInfo.IsDir() {
		t.Error("want a directory")
		return
	}

	// Check the log directory name
	if dirInfo.Name() != wantLogDirBaseName {
		t.Errorf("want directory %s got %s", wantLogDirBaseName, dirInfo.Name())
		return
	}

	// Scan the log directory.
	files, fe := os.ReadDir(logDirPathName)
	if fe != nil {
		t.Errorf("error scanning directory %s - %v", testDirectoryName, fe)
		return
	}

	// Check that one log file was created and contains the wanted contents.
	if len(files) != 1 {
		t.Errorf("directory %s contains %d files.  Should contain exactly one.", testDirectoryName, len(files))
		return
	}

	if files[0].Name() != wantLogFileName {
		t.Errorf("directory %s contains file \"%s\", want \"%s\".", testDirectoryName, files[0].Name(), wantLogFileName)
		return
	}

	// Check the contents.
	inputFile, err := os.OpenFile(logFilePathName, os.O_RDONLY, 0644)
	if err != nil {
		t.Error(err)
		return
	}
	defer inputFile.Close()

	b := make([]byte, 8096)
	length, err := inputFile.Read(b)
	if err != nil {
		t.Errorf("error reading logfile back - %v", err)
		return
	}
	if length != len(buffer) {
		t.Errorf("logfile contains %d bytes - want %d", length, len(buffer))
		return
	}

	contents := string(b[:length])

	if wantContents != contents {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", contents, wantContents)
		return
	}

	// The rest of the test only works on a Posix system.  On a Windows system, New creates
	// the directory and the logfile but it cannot set permissions, user or group.
	if ps.OSName != "windows" {
		// On a POSIX system, check the owner, permissions etc of the files.

		wantUserID, uide := getUserIDFromName(userName)
		if uide != nil {
			t.Error(uide)
			return
		}
		fmt.Printf("user ID %d\n", wantUserID)

		wantGroupID, gide := getGroupIDFromName(group)
		if gide != nil {
			t.Error(gide)
			return
		}
		fmt.Printf("group ID %d\n", wantGroupID)

		// The log directory.
		d, de := os.Open(logDirPathName)
		if de != nil {
			t.Error(de)
			return
		}
		dStat, dStatErr := ps.Stat(d)
		if dStatErr != nil {
			t.Error(dStatErr)
		}

		// Chec the user that owns the directory.
		if int(dStat.Uid) != wantUserID {
			t.Errorf("want %d got %d", wantUserID, dStat.Uid)
			return
		}

		// Check the group that the directory is in.
		if int(dStat.Gid) != wantGroupID {
			t.Errorf("want %d got %d", wantGroupID, dStat.Gid)
			return
		}

		// Check the directory permissions.
		dirPermissions := os.FileMode(dStat.Mode) & os.ModePerm
		if dirPermissions != wantDirPermissions {
			t.Errorf("want 0%o got 0%o", wantFilePermissions, dirPermissions)
			return
		}

		// The log file.
		fStat, fStatErr := ps.Stat(inputFile)
		if fStatErr != nil {
			t.Error(fStatErr)
			return
		}

		// Check the owner of the log file.
		if int(fStat.Uid) != wantUserID {
			t.Errorf("want %d got %d", wantUserID, dStat.Uid)
			return
		}

		// Check the group that the file is in.
		if int(fStat.Gid) != wantGroupID {
			t.Errorf("want %d got %d", wantGroupID, fStat.Gid)
			return
		}

		// Check the log file permissions.
		filePermissions := os.FileMode(fStat.Mode) & os.ModePerm
		if filePermissions != wantFilePermissions {
			t.Errorf("want o%o got o%o", wantFilePermissions, filePermissions)
			return
		}
	}
}

// TestWaitToRotate checks that waitToRotate waits for the right time.
func TestWaitToRotate(t *testing.T) {
	// Set a time just before midnight, run the test and check the elapsed time.  If
	// the system is not busy it should be a little more than 1500 ms, but we can't
	// predict how much more.  Checking that it's more than 500 ms is the best we
	// do.

	// 500 millseconds before midnight
	const smallDuration = time.Millisecond * 500
	locationParis, _ := time.LoadLocation("Europe/Paris")
	startTime := time.Date(2020, time.February, 14, 23, 59, 59, int(smallDuration), locationParis)

	const minDuration = extraDuration - smallDuration

	// Test.
	waitToRotate(startTime)

	// Check.
	now := time.Now()
	elapsed := now.Sub(startTime)

	if elapsed < minDuration {
		t.Errorf("want at least %d got %d", minDuration, elapsed)
		return
	}
}

// TestRollover checks that the log rollover mechanism creates a new file each day.
func TestRollover(t *testing.T) {

	// This test uses the filestore.

	directoryName, err := CreateWorkingDirectory()
	if err != nil {
		t.Errorf("createWorkingDirectory failed - %v", err)
		return
	}
	defer RemoveWorkingDirectory(directoryName)

	const wantMessage1 = "hello"
	const wantFilename1 = "foo.2020-02-14.bar"
	buffer1 := []byte(wantMessage1)
	const wantMessage2 = "world"
	buffer2 := []byte(wantMessage2)
	const wantFilename2 = "foo.2020-02-15.bar"

	locationParis, _ := time.LoadLocation("Europe/Paris")
	// 500 millseconds before midnight
	now := time.Date(2020, time.February, 14, 23, 59, 59, int(time.Millisecond)*500, locationParis)
	// Midnight at the start of the next day.
	tomorrow := time.Date(2020, time.February, 15, 14, 35, 0, 0, locationParis)

	writer := New(now, ".", "foo.", ".bar")

	// This should write to wantFilename1.
	n, err := writer.Write(buffer1)
	if err != nil {
		t.Errorf("Write failed - %v", err)
		return
	}

	if n != len(buffer1) {
		t.Errorf("Write returns %d - want %d", n, len(buffer1))
		return
	}

	// roll the log over.
	writer.rotateLogs(tomorrow)

	// This should write to wantFilename2.
	n, err = writer.Write(buffer2)
	if err != nil {
		t.Errorf("Write failed - %v", err)
		return
	}

	if n != len(buffer2) {
		t.Errorf("Write returns %d - want %d", n, len(buffer2))
		return
	}

	// The current directory should contain wantLogfile1 and wantLogfile2.
	// Check that tthe two files exist.
	files, err := os.ReadDir(directoryName)
	if err != nil {
		t.Errorf("error scanning directory %s - %s", directoryName, err.Error())
		return
	}

	if len(files) != 2 {
		t.Errorf("directory %s contains %d files.  Should contain just 2.",
			directoryName, len(files))
		return
	}

	if files[0].Name() != wantFilename1 &&
		files[0].Name() != wantFilename2 {

		t.Errorf("directory %s contains file \"%s\", want \"%s\" or \"%s\".",
			directoryName, files[0].Name(), wantFilename1, wantFilename2)
		return
	}

	if files[1].Name() != wantFilename1 &&
		files[1].Name() != wantFilename2 {

		t.Errorf("directory %s contains file \"%s\", want \"%s\" or \"%s\".",
			directoryName, files[1].Name(), wantFilename1, wantFilename2)
		return
	}

	// Check the contents.
	wantPathName1 := directoryName + "/" + wantFilename1
	inputFile, err := os.OpenFile(wantPathName1, os.O_RDONLY, 0644)
	if err != nil {
		t.Error(err)
		return
	}
	defer inputFile.Close()
	b := make([]byte, 8096)
	length, err := inputFile.Read(b)
	if err != nil {
		t.Errorf("error reading logfile %s back - %v", wantFilename1, err)
		return
	}
	if length != len(wantMessage1) {
		t.Errorf("logfile contains %d bytes - want %d", length, len(wantMessage1))
		return
	}
	result1 := string(b[:length])
	if result1 != wantMessage1 {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", result1, wantMessage1)
		return
	}

	wantPathName2 := directoryName + "/" + wantFilename2
	inputFile2, err := os.OpenFile(wantPathName2, os.O_RDONLY, 0644)
	if err != nil {
		t.Error(err)
		return
	}
	defer inputFile2.Close()
	length, err = inputFile2.Read(b)
	if err != nil {
		t.Errorf("error reading logfile %s back - %v", wantFilename2, err)
	}
	if length != len(buffer2) {
		t.Errorf("logfile contains %d bytes - want %d", length, len(buffer2))
	}
	result2 := string(b[:length])
	if result2 != wantMessage2 {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", result2, wantMessage2)
	}
}

// TestRolloverWithLongDelay checks that the log rollover mechanism produces
// the correct datestamp when it's run very late and the day has
// moved on further.
func TestRolloverWithLongDelay(t *testing.T) {

	// This test uses the filestore.

	const wantMessage1 = "hello"
	// buffer1 := []byte(message1)
	const wantMessage2 = "world"
	// buffer2 := []byte(wantMessage)
	const wantLogFilename1 = "foo.2020-02-14.bar"
	const wantLogFilename2 = "foo.2020-03-15.bar"
	const wantLogDirPermissions = 0700
	const wantLogFilePermissions = 0600
	const wantLinuxUser = "bin"     // This user exists under our target system Linux.
	const wantLinuxGroup = "daemon" // This group exists under our target system Linux.

	wantLogDir, mde := MakeUUID()
	if mde != nil {
		t.Error(mde)
		return
	}

	testDirectoryName, re1 := CreateWorkingDirectory()
	if re1 != nil {
		t.Errorf("createWorkingDirectory failed - %v", re1)
	}
	defer RemoveWorkingDirectory(testDirectoryName)

	fullPathnameLogDir := testDirectoryName + "/" + wantLogDir + "/"

	locationLondon, _ := time.LoadLocation("Europe/London")
	now := time.Date(2020, time.February, 14, 23, 59, 59, int(time.Millisecond)*800,
		locationLondon)
	nextMonth := time.Date(2020, time.March, 15, 12, 0, 0, 0, locationLondon)

	// Test.
	var writer *Writer
	if ps.OSName == "windows" {
		writer = New(now, wantLogDir, "foo.", ".bar",
			wantLogDirPermissions, wantLogFilePermissions)
	} else {
		writer = New(now, wantLogDir, "foo.", ".bar",
			wantLogDirPermissions, wantLogFilePermissions, wantLinuxUser, wantLinuxGroup)
	}

	// Write to the log for the 14th.
	n1, re1 := writer.Write([]byte(wantMessage1))
	if re1 != nil {
		t.Errorf("Write failed - %v", re1)
	}

	// roll the log over.
	writer.rotateLogs(nextMonth)

	// This should write to wantLogFilename2.
	n2, de := writer.Write([]byte(wantMessage2))
	if de != nil {
		t.Errorf("Write failed - %v", de)
	}

	// Check.

	if n1 != len(wantMessage1) {
		t.Errorf("Write returns %d - want %d", n1, len(wantMessage1))
		return
	}

	if n2 != len([]byte(wantMessage2)) {
		t.Errorf("Write returns %d - want %d", n2, len([]byte(wantMessage2)))
		return
	}

	// The current directory should contain directory wantLogDir (permissions 0700)
	// containing wantLogfilename1 and wantLogFilename2.

	// Check the log directory.
	fileList1, de1 := os.ReadDir(testDirectoryName)
	if de1 != nil {
		t.Errorf("error scanning directory %s - %v", testDirectoryName, de1)
		return
	}

	if len(fileList1) != 1 {
		t.Errorf("directory %s contains %d files.  Should contain just 1.",
			testDirectoryName, len(fileList1))
		return
	}

	if fileList1[0].Name() != wantLogDir {
		t.Errorf("want log dir %s, got %s", wantLogDir, fileList1[0])
		return
	}

	if !fileList1[0].IsDir() {
		t.Errorf("log dir %s should be a directory", fileList1[0].Name())
		return
	}

	// Check that the two log files exist.
	dirList, de := os.ReadDir(fullPathnameLogDir)
	if de != nil {
		t.Errorf("error scanning directory %s - %v", testDirectoryName, de)
		return
	}

	// There should be two files.
	if len(dirList) != 2 {
		t.Errorf("directory %s contains %d files.  Should contain just 2.",
			testDirectoryName, len(dirList))
		return
	}

	// Both files should be plain text.
	dirEntry1 := dirList[0]
	if !dirEntry1.Type().IsRegular() {
		t.Errorf("want %s to be a plain file", dirEntry1.Name())
	}
	dirEntry2 := dirList[1]
	if !dirEntry2.Type().IsRegular() {
		t.Errorf("want %s to be a plain file", dirEntry2.Name())
	}

	// Check the names of the files in the log directory.  Don't assume that they are in
	// alphabetical order.
	if !((dirEntry1.Name() == wantLogFilename1 &&
		dirEntry2.Name() == wantLogFilename2) ||
		(dirEntry1.Name() == wantLogFilename2 &&
			dirEntry2.Name() == wantLogFilename1)) {

		t.Errorf("want file %s and %s, got %s and %s",
			wantLogFilename1, wantLogFilename2, dirEntry1.Name(), dirEntry2.Name())
		return
	}

	// Check the contents of the two files.  Use the full path name, which adds another
	// check.
	wantPathName1 := fullPathnameLogDir + wantLogFilename1
	inputFile1, oe1 := os.OpenFile(wantPathName1, os.O_RDONLY, 0644)
	if oe1 != nil {
		t.Error(oe1)
	}
	defer inputFile1.Close()

	b1 := make([]byte, 8096)
	length1, re1 := inputFile1.Read(b1)
	if re1 != nil {
		t.Errorf("error reading logfile %s back - %v", wantLogFilename1, re1)
		return
	}
	if length1 != len([]byte(wantMessage1)) {
		t.Errorf("logfile contains %d bytes - want %d", length1, len([]byte(wantMessage1)))
		return
	}
	result1 := string(b1[:length1])
	if result1 != wantMessage1 {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", result1, wantMessage1)
		return
	}

	wantPathName2 := fullPathnameLogDir + wantLogFilename2
	inputFile2, oe2 := os.OpenFile(wantPathName2, os.O_RDONLY, 0644)
	if oe2 != nil {
		t.Error(oe2)
		return
	}
	defer inputFile2.Close()

	b2 := make([]byte, 8096)
	length2, re2 := inputFile2.Read(b2)
	if re2 != nil {
		t.Errorf("error reading logfile %s back - %v", wantLogFilename2, re2)
		return
	}
	if length2 != len(wantMessage2) {
		t.Errorf("logfile contains %d bytes - want %d", length2, len(wantMessage2))
		return
	}
	result2 := string(b2[:length2])
	if result2 != wantMessage2 {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", result2, wantMessage2)
		return
	}

	if ps.OSName != "windows" {

		// Under Linux, check the permissions of the log directory.

		dirInfo, ie1 := fileList1[0].Info()
		if ie1 != nil {
			t.Error(ie1)
		}

		perms := dirInfo.Mode() & os.ModePerm
		if perms != wantLogDirPermissions {
			t.Errorf("want permissions 0%o, got 0%o", wantLogDirPermissions, perms)
			return
		}

		// Check the permissions and the ownership of the log files.

		logFileInfo1, ie3 := dirEntry1.Info()
		if ie3 != nil {
			t.Error(ie3)
		}

		// Check the permissions of the first file.
		perms1 := logFileInfo1.Mode() & os.ModePerm
		if perms1 != wantLogFilePermissions {
			t.Errorf("want permissions 0%o, got 0%o", wantLogFilePermissions, perms1)
			return
		}

		stat1, e1 := ps.Stat(inputFile1)
		if e1 != nil {
			t.Error(e1)
		}

		// Check the owner of the first log file.
		owner1, ue := getUserFromID(stat1.Uid)
		if ue != nil {
			t.Error(ue)
		}

		if owner1.Name != wantLinuxUser {
			t.Errorf("want %s user got %s", wantLinuxUser, owner1.Name)
			return
		}

		// Check the group of the first log file.
		ownerGroup1, ge := getGroupFromID(stat1.Gid)
		if ge != nil {
			t.Error(ge)
			return
		}

		if ownerGroup1.Name != wantLinuxGroup {
			t.Errorf("want group %s got %s", wantLinuxGroup, ownerGroup1.Name)
			return
		}

		logFileInfo2, ie3 := dirEntry2.Info()
		if ie3 != nil {
			t.Error(ie3)
		}
		perms2 := logFileInfo2.Mode() & os.ModePerm
		if perms2 != wantLogFilePermissions {
			t.Errorf("want permissions 0%o, got 0%o", wantLogFilePermissions, perms2)
			return
		}

		stat2, e2 := ps.Stat(inputFile2)
		if e2 != nil {
			t.Error(e2)
		}
		owner2, ue := getUserFromID(stat2.Uid)
		if ue != nil {
			t.Error(ue)
		}

		if owner2.Name != wantLinuxUser {
			t.Errorf("want user %s got %s", wantLinuxUser, owner2.Name)
			return
		}

		ownerGroup2, ge := getGroupFromID(stat2.Gid)
		if ge != nil {
			t.Error(ge)
			return
		}

		if ownerGroup2.Name != wantLinuxGroup {
			t.Errorf("want group %s got %s", wantLinuxGroup, ownerGroup2.Name)
			return
		}
	}
}

// TestAppendOnRestart checks that if the program creates a log file for the day,
// then crashes and restarts, the Writer appends to the existing file rather than
// overwriting it.
func TestAppendOnRestart(t *testing.T) {

	// NOTE:  this test uses the filestore.

	const wantMessage1 = "goodbye "
	buffer1 := []byte(wantMessage1)
	const wantMessage2 = "cruel world"
	buffer2 := []byte(wantMessage2)
	const wantFilename = "log.2020-02-14.txt"
	const wantFirstContents = "goodbye "
	const wantFinalContents = "goodbye cruel world"

	directoryName, err := CreateWorkingDirectory()
	if err != nil {
		t.Errorf("createWorkingDirectory failed - %v", err)
		return
	}
	defer RemoveWorkingDirectory(directoryName)

	locationUTC, err := time.LoadLocation("UTC")
	if err != nil {
		t.Errorf("error while loading UTC timezone - %v", err)
		return
	}

	// Write some text to the logger.
	// That should create a file for today.
	now := time.Date(2020, time.February, 14, 0, 1, 30, 0, locationUTC)
	writer1 := New(now, ".", "log.", ".txt")
	n, err := writer1.Write(buffer1)
	if err != nil {
		t.Errorf("Write failed - %v", err)
		return
	}
	if n != len(wantMessage1) {
		t.Errorf("Write returns %d - want %d", n, len(wantMessage1))
		return
	}

	inputFile, err := os.OpenFile(wantFilename, os.O_RDONLY, 0644)
	if err != nil {
		t.Errorf("Failed to open file %s - %v", wantFilename, err)
		return
	}
	defer inputFile.Close()
	inputBuffer := make([]byte, 8096)
	n, err = inputFile.Read(inputBuffer)
	if err != nil {
		t.Errorf("error reading logfile back - %v", err)
		return
	}
	contents := string(inputBuffer[:n])
	if contents != wantFirstContents {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", contents, wantFirstContents)
		return
	}

	// Create a new writer.  On the first call it will behave as on system startup.  It should
	// append to the existing daily log.
	now = time.Date(2020, time.February, 14, 0, 2, 30, 0, locationUTC)
	writer2 := New(now, ".", "log.", ".txt")
	n, err = writer2.Write(buffer2)
	if err != nil {
		t.Errorf("Write failed - %v", err)
		return
	}
	if n != len(buffer2) {
		t.Errorf("Write returns %d - want %d", n, len(wantMessage2))
		return
	}

	// Check that only one log file was created and contains the want contents.
	files, err := os.ReadDir(directoryName)
	if err != nil {
		t.Errorf("error scanning directory %s - %s", directoryName, err.Error())
		return
	}

	if len(files) != 1 {
		t.Errorf("directory %s contains %d files.  Should contain exactly one.", directoryName, len(files))
		return
	}

	if files[0].Name() != wantFilename {
		t.Errorf("directory %s contains file \"%s\", want \"%s\".", directoryName, files[0].Name(), wantFilename)
		return
	}

	// Check the contents.  It should be the result of the two Write calls.

	inputFile, err = os.OpenFile(wantFilename, os.O_RDONLY, 0644)
	defer inputFile.Close()
	inputBuffer = make([]byte, 8096)
	n, err = inputFile.Read(inputBuffer)
	if err != nil {
		t.Errorf("error reading logfile back - %v", err)
		return
	}

	contents = string(inputBuffer[:n])
	if contents != wantFinalContents {
		t.Errorf("logfile contains \"%s\" - want \"%s\"", contents, wantFinalContents)
		return
	}
}

// MakeUUID creates a UUID.  The technique uses a random source so different UUIDs are produced
// on different runs.
func MakeUUID() (string, error) {
	uid, randError := uuid.NewRandom()
	if randError != nil {
		// This is extremely unlikely to ever happen.
		return "", randError
	}
	return uid.String(), nil
}

// CreateWorkingDirectory create a working directory, makes it the current
// directory and returns its name.  To ensure its removal, use a deferred
// run of RemoveWorkingDirectory.
func CreateWorkingDirectory() (string, error) {
	// Create a randomly-generated unique name.
	name, ue := MakeUUID()
	if ue != nil {
		return "", ue
	}
	// Create a directory of that name in the system's temp space.  Under UNIX, that's
	// "/tmp", under Windows it's something else.
	directoryName, mde := os.MkdirTemp("", name)
	if mde != nil {
		return "", mde
	}

	// Change to that directory.
	cde := os.Chdir(directoryName)
	if cde != nil {
		return "", cde
	}
	return directoryName, nil
}

// RemoveWorkingDirectory removes the working directory and any files in it.
func RemoveWorkingDirectory(directoryName string) error {
	err := os.RemoveAll(directoryName)
	if err != nil {
		return err
	}
	return nil
}
