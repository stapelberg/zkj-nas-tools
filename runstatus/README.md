HTTP server which displays “running” or “notrunning”, depending on whether at
least one process with the name specified by `-program` exists (looking at
`/proc/<pid>/comm`).

This is useful in combination with avr-x1100w (the orchestration tool). When
using `runstatus -program=i3lock`, the avr-x1100w tool can figure out whether
the screen of my workstation is locked.
