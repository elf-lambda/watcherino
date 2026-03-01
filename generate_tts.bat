@echo off
setlocal EnableDelayedExpansion

set PIPER_EXE=tools\piper\piper.exe
set PIPER_MODEL=tools\piper\en_US-joe-medium.onnx

set TTSPATH=tts\\
set TTSMESSAGE=is now streaming.

for /f "usebackq tokens=1,* delims==" %%A in ("config.txt") do (
    set KEY=%%A
    set VAL=%%B
    if not "!KEY:~0,1!"=="#" if not "!KEY!"=="" (
        if "!KEY!"=="$ttspath"    set TTSPATH=%%B
        if "!KEY!"=="$ttsmessage" set TTSMESSAGE=%%B
    )
)

if not exist "%TTSPATH%" mkdir "%TTSPATH%"

for /f "usebackq tokens=1,* delims==" %%A in ("config.txt") do (
    set KEY=%%A
    set VAL=%%B
    if not "!KEY:~0,1!"=="#" if not "!KEY:~0,1!"=="$" if not "!KEY!"=="" (

        set TEXT=!KEY! %TTSMESSAGE%
        set OUTFILE=%TTSPATH%!KEY!.wav
        if not exist "!OUTFILE!" (
            echo Generating: !OUTFILE! ^("!TEXT!"^)
            echo !TEXT! | "%PIPER_EXE%" --model "%PIPER_MODEL%" --output_file "!OUTFILE!"
        ) else (
            echo Skipping ^(exists^): !OUTFILE!
        )

    )
)
echo Done.