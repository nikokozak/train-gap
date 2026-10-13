# The README's demo GIF. Run from the repo root: vhs docs/demo.tape (https://github.com/charmbracelet/vhs)
# At 8x speed the L stands at 8 Av for 4 s, loses service in the tunnel to 6 Av for 6 s, and
# arrives at 16 s. A request every second shows connections wait, then finish.
Output docs/demo.gif

Set Shell bash
Set Theme "TokyoNight"
Set FontSize 15
Set Width 860
Set Height 480
Set Padding 12
Set TypingSpeed 50ms

Hide
Type "go build -o /tmp/train-gap-demo/train-gap . && export PATH=/tmp/train-gap-demo:$PATH" Enter
Type "(sleep 2; while true; do for host in example.com www.google.com api.github.com; do curl -s -o /dev/null -x http://localhost:1080 https://$host & sleep 1; done; done) >/dev/null 2>&1 &" Enter
Type "clear" Enter
Show

Type "train-gap --speed 8"
Sleep 400ms
Enter
Sleep 19s
Type "q"
Sleep 300ms
