# vhs docs/demo.tape
#
# no ollama, gpu or traffic needed, mtop --demo fakes all of it. Run from
# the repo root with a built ./mtop.exe (./mtop elsewhere). The scenario
# loops every 40s and the cues below are tuned to it: live rows from the
# start, the cpu verdict on qwen3:32b, the thermal throttle line from
# ~20s, the queue verdict on the llama.cpp row from ~32s. The window has
# to be at least 100x30; Height 720 is about 31 rows at FontSize 18.
#
# vhs 0.12 on windows records the frames but doesn't write the gif, so
# the frames go to a folder too (on the same drive as %TEMP%, or they
# silently go missing):
#   ffmpeg -y -framerate 10 -i $TEMP/mtop-frames-demo/frame-text-%05d.png -framerate 10 -i $TEMP/mtop-frames-demo/frame-cursor-%05d.png \
#     -filter_complex "[0][1]overlay,crop=1111:612:0:0,fps=8,split[a][b];[a]palettegen=max_colors=40:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" docs/img/demo.gif
#
# the crop drops the empty bottom of the 31-row window (the footer ends at y=600).

# machine-specific: edit to a folder on the same drive as %TEMP%
Output "C:/Users/Elads/AppData/Local/Temp/mtop-frames-demo/"
Output docs/img/demo.gif

Set Shell "powershell"
Set FontFamily "Cascadia Mono"
Set FontSize 18
Set Width 1165
Set Height 720
Set Padding 12
Set Framerate 10
Set TypingSpeed 0
Env COLORTERM "truecolor"
Set Theme { "name": "Campbell", "black": "#0C0C0C", "red": "#C50F1F", "green": "#13A10E", "yellow": "#C19C00", "blue": "#0037DA", "magenta": "#881798", "cyan": "#3A96DD", "white": "#CCCCCC", "brightBlack": "#767676", "brightRed": "#E74856", "brightGreen": "#16C60C", "brightYellow": "#F9F1A5", "brightBlue": "#3B78FF", "brightMagenta": "#B4009E", "brightCyan": "#61D6D6", "brightWhite": "#F2F2F2", "background": "#0C0C0C", "foreground": "#CCCCCC", "selection": "#FFFFFF", "cursor": "#FFFFFF" }

# vhs sets NO_COLOR, which turns mtop monochrome
Hide
Type "Remove-Item env:NO_COLOR; cls; ./mtop.exe --demo"
Enter
Sleep 1.5s
Show

# live rows streaming, then the 32b offload verdict
Sleep 3s
Type "j"
Type "j"
Sleep 17s

# thermal throttle line is up by now (3-poll debounce); the queue verdict
# on the llama.cpp row needs ~32s into the loop
Type "j"
Sleep 14.5s
Type "q"
Sleep 500ms
