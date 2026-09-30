# vhs docs/demo.tape
#
# needs ollama with qwen3:0.6b loaded partly on cpu (num_gpu 18) and
# smollm2:135m at num_ctx 512, plus some traffic through 127.0.0.1:4321
# while it records. Change the paths below to your checkout.
#
# vhs 0.12 on windows records the frames but doesn't write the gif, so
# the frames go to a folder too (on the same drive as %TEMP%, or they
# silently go missing):
#   ffmpeg -framerate 10 -i $TEMP/mtop-frames/frame-text-%05d.png -framerate 10 -i $TEMP/mtop-frames/frame-cursor-%05d.png \
#     -filter_complex "[0][1]overlay,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" docs/img/demo.gif

Output "C:/Users/Elads/AppData/Local/Temp/mtop-frames/"
Output docs/img/demo.gif

Set Shell "powershell"
Set FontFamily "Cascadia Mono"
Set FontSize 13
Set Width 1200
Set Height 400
Set Padding 12
Set Framerate 10
Set TypingSpeed 0
Env COLORTERM "truecolor"
Set Theme { "name": "Campbell", "black": "#0C0C0C", "red": "#C50F1F", "green": "#13A10E", "yellow": "#C19C00", "blue": "#0037DA", "magenta": "#881798", "cyan": "#3A96DD", "white": "#CCCCCC", "brightBlack": "#767676", "brightRed": "#E74856", "brightGreen": "#16C60C", "brightYellow": "#F9F1A5", "brightBlue": "#3B78FF", "brightMagenta": "#B4009E", "brightCyan": "#61D6D6", "brightWhite": "#F2F2F2", "background": "#0C0C0C", "foreground": "#CCCCCC", "selection": "#FFFFFF", "cursor": "#FFFFFF" }

# vhs sets NO_COLOR, which turns mtop monochrome
Hide
Type "Remove-Item env:NO_COLOR; cls; D:/Projects/wt/mtop-release/mtop.exe -inspect"
Enter
Sleep 1.5s
Type "j"
Sleep 300ms
Show

Sleep 9s
Type "c"
Sleep 3s
Type "c"
Sleep 1.5s
Type "i"
Sleep 3.5s
Type "i"
Sleep 1s
Type "q"
Sleep 500ms
