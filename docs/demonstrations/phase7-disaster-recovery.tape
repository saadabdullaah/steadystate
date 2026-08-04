Output .artifacts/phase7/acceptance/phase7-disaster-recovery.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set TypingSpeed 10ms
Set Framerate 2
Set PlaybackSpeed 1.0
Set WaitTimeout 1m

Type "cat .artifacts/phase7/acceptance/transcript.txt"
Enter
Wait+Screen /RESULT .* PASSED/
Sleep 3s
