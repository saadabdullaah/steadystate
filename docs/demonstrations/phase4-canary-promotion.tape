Output docs/demonstrations/phase4-canary-promotion.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 20
Set Margin 24
Set MarginFill "#111827"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 5ms
Set WaitTimeout 20m
Set Framerate 2
Set PlaybackSpeed 8.0

Type "pwsh -NoProfile -File scripts/phase4-acceptance.ps1 -Stage Promote -Profile standard"
Enter
Wait+Screen /PHASE4_PROMOTION_RESULT_(PASSED|FAILED)/
Sleep 3s
