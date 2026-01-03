# dailylogger

A go writer that writes to a new file each day.

The daily logger creates a log file with a configurable name
including a YYYY-MM-DD date stamp.  
If writing continues after midnight,
a new log file is created and the writer
sends the output to that for the rest of the day.

The leading part and the trailing part 
of the log file name are supplied when the wrter is created.
For example, if the leader is "payments." and the trailer is ".log",
the log file for the 14th February 2026 will be
"payments.2026-02-14.log".

A program running as root may create the log file
and then switch to running as a less privileged user.
In that case the user, group and permissions 
of the log file can be set when the logger is created.

Once the writer is created,
it can be incorporated into a SLOG logger lile so:

    logger := slog.New(slog.NewTextHandler(dailyLogWriter, nil))


    
    
