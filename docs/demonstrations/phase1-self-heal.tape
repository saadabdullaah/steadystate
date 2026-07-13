Output docs/demonstrations/phase1-self-heal.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 20
Set Margin 24
Set MarginFill "#111827"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 8ms
Set WaitTimeout 2m

Type "pwsh -NoProfile -File scripts/dev.ps1 demo-self-heal -HttpPort 8080 -EvidencePath .artifacts/phase1/acceptance.json"
Enter
Wait+Screen /SteadyState Application self-heal demonstration passed\./
Sleep 3s
