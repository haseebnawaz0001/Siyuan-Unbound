@echo off

echo Usage: .\win-build.bat [--target=^<target^>]
echo Options:
echo   --target=^<target^>  Build target: amd64, arm64, appx-amd64, appx-arm64, or all (default: all)
echo.

REM Save initial directory
set "INITIAL_DIR=%cd%"

REM Get script directory
set "SCRIPT_DIR=%~dp0"
REM Remove trailing backslash
if "%SCRIPT_DIR:~-1%"=="\" set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

REM Get project root (parent of script directory)
for %%I in ("%SCRIPT_DIR%\..") do set "PROJECT_ROOT=%%~fI"

set "TARGET=all"

:parse_args
if "%1"=="" goto :end_parse_args
if "%1"=="--target" (
    if "%2"=="" (
        echo Error: --target option requires an equal sign and value
        echo Usage: --target=^<target^>
        echo Example: --target=amd64
        exit /b 1
    ) else (
        if not "%2"=="amd64" if not "%2"=="arm64" if not "%2"=="appx-amd64" if not "%2"=="appx-arm64" if not "%2"=="all" (
            echo Error: Invalid target '%2'
            echo Valid targets are: amd64, arm64, appx-amd64, appx-arm64, all
            exit /b 1
        )
        set "TARGET=%2"
        shift
        shift
        goto :parse_args
    )
) else (
    @REM Skip unknown options
    shift
    goto :parse_args
)
:end_parse_args

if "%TARGET%"=="amd64" (
    set BUILD_AMD64=1
) else if "%TARGET%"=="arm64" (
    set BUILD_ARM64=1
) else if "%TARGET%"=="appx-amd64" (
    REM The appx stage depends on the win-unpacked directory produced by electron-builder, so amd64 electron must also be built
    set BUILD_AMD64=1
    set BUILD_APPX_AMD64=1
) else if "%TARGET%"=="appx-arm64" (
    REM The appx stage depends on the win-arm64-unpacked directory produced by electron-builder, so arm64 electron must also be built
    set BUILD_ARM64=1
    set BUILD_APPX_ARM64=1
) else (
    REM all: build everything
    set BUILD_AMD64=1
    set BUILD_ARM64=1
    set BUILD_APPX_AMD64=1
    set BUILD_APPX_ARM64=1
)

echo Cleaning Builds
rmdir /S /Q "%PROJECT_ROOT%\app\build" 1>nul
rmdir /S /Q "%PROJECT_ROOT%\app\kernel" 1>nul
rmdir /S /Q "%PROJECT_ROOT%\app\kernel-arm64" 1>nul

echo.
echo Building UI
cd /d "%PROJECT_ROOT%\app"
if errorlevel 1 (
    exit /b %errorlevel%
)
call pnpm install
if errorlevel 1 (
    exit /b %errorlevel%
)
call pnpm run install:electron
if errorlevel 1 (
    exit /b %errorlevel%
)
call pnpm run build
if errorlevel 1 (
    exit /b %errorlevel%
)

echo.
echo Building Kernel
@REM the C compiler "gcc" is necessary https://sourceforge.net/projects/mingw-w64/files/mingw-w64/
cd /d "%PROJECT_ROOT%\kernel"
if errorlevel 1 (
    exit /b %errorlevel%
)
go version
set GO111MODULE=on
set GOPROXY=https://mirrors.aliyun.com/goproxy/,https://goproxy.cn,direct
set CGO_ENABLED=1
set GOOS=windows

echo Setting up goversioninfo
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
if errorlevel 1 (
    exit /b %errorlevel%
)
set "GO_BIN="
for /f "delims=" %%I in ('go env GOBIN') do set "GO_BIN=%%I"
if not defined GO_BIN (
    for /f "tokens=1 delims=;" %%I in ('go env GOPATH') do set "GO_BIN=%%I\bin"
)

@REM you can use `go mod tidy` to update kernel dependency before build
@REM you can use `go generate` instead (need add something in main.go)
"%GO_BIN%\goversioninfo.exe" -platform-specific=true -icon=resource/icon.ico -manifest=resource/goversioninfo.exe.manifest
if errorlevel 1 (
    exit /b %errorlevel%
)

if defined BUILD_AMD64 (
    echo.
    echo Building Kernel amd64
    set GOARCH=amd64
    go build -tags "fts5 sqlcipher" -o "%PROJECT_ROOT%\app\kernel\SiYuan-Kernel.exe" -ldflags "-s -w" .
    if errorlevel 1 (
        exit /b %errorlevel%
    )
)
if defined BUILD_ARM64 (
    echo.
    echo Building Kernel arm64
    set GOARCH=arm64
    call :resolve_arm64_cc
    if errorlevel 1 (
        exit /b 1
    )
    go build -tags "fts5 sqlcipher" -o "%PROJECT_ROOT%\app\kernel-arm64\SiYuan-Kernel.exe" -ldflags "-s -w" .
    if errorlevel 1 (
        exit /b %errorlevel%
    )
)

if defined BUILD_AMD64 goto electron
if defined BUILD_ARM64 goto electron
goto :skipelectron
:electron
echo.
echo Building Electron App
cd /d "%PROJECT_ROOT%\app"
if errorlevel 1 (
    exit /b %errorlevel%
)
if defined BUILD_AMD64 (
    echo.
    echo Building Electron App amd64
    copy "%PROJECT_ROOT%\app\elevator\elevator-amd64.exe" "%PROJECT_ROOT%\app\kernel\elevator.exe"
    call pnpm run dist
    if errorlevel 1 (
        exit /b %errorlevel%
    )
)
if defined BUILD_ARM64 (
    echo.
    echo Building Electron App arm64
    copy "%PROJECT_ROOT%\app\elevator\elevator-arm64.exe" "%PROJECT_ROOT%\app\kernel-arm64\elevator.exe"
    call pnpm run dist-arm64
    if errorlevel 1 (
        exit /b %errorlevel%
    )
)
:skipelectron

if defined BUILD_APPX_AMD64 goto appx
if defined BUILD_APPX_ARM64 goto appx
goto :skipappx
:appx
echo.
echo Building Appx
cd /d "%PROJECT_ROOT%"
if errorlevel 1 (
    exit /b %errorlevel%
)
if defined BUILD_APPX_AMD64 (
    echo.
    echo Building Appx amd64
    echo Building Appx amd64 should be disabled if you do not need it. Not configured correctly will lead to build failures
    cd . > "%PROJECT_ROOT%\app\build\win-unpacked\resources\ms-store"
    if errorlevel 1 (
        exit /b %errorlevel%
    )
    call electron-windows-store --input-directory "%PROJECT_ROOT%\app\build\win-unpacked" --output-directory "%PROJECT_ROOT%\app\build" --package-version 1.0.0.0 --package-name SiYuan --manifest "%PROJECT_ROOT%\app\appx\AppxManifest.xml" --assets "%PROJECT_ROOT%\app\appx\assets" --make-pri true

    rmdir /S /Q "%PROJECT_ROOT%\app\build\pre-appx" 1>nul
)
if defined BUILD_APPX_ARM64 (
    echo.
    echo Building Appx arm64
    echo Building Appx arm64 should be disabled if you do not need it. Not configured correctly will lead to build failures
    cd . > "%PROJECT_ROOT%\app\build\win-arm64-unpacked\resources\ms-store"
    if errorlevel 1 (
        exit /b %errorlevel%
    )
    call electron-windows-store --input-directory "%PROJECT_ROOT%\app\build\win-arm64-unpacked" --output-directory "%PROJECT_ROOT%\app\build" --package-version 1.0.0.0 --package-name SiYuan-arm64 --manifest "%PROJECT_ROOT%\app\appx\AppxManifest-arm64.xml" --assets "%PROJECT_ROOT%\app\appx\assets" --make-pri true

    rmdir /S /Q "%PROJECT_ROOT%\app\build\pre-appx" 1>nul
)
:skipappx

REM Create the hard link only after packaging is done, to avoid bundling siyuan.exe into the installer
if defined BUILD_AMD64 (
    mklink /H "%PROJECT_ROOT%\app\kernel\siyuan.exe" "%PROJECT_ROOT%\app\kernel\SiYuan-Kernel.exe"
)
if defined BUILD_ARM64 (
    mklink /H "%PROJECT_ROOT%\app\kernel-arm64\siyuan.exe" "%PROJECT_ROOT%\app\kernel-arm64\SiYuan-Kernel.exe"
)

echo.
echo ==============================
echo       Build successful!
echo ==============================

REM Return to the initial directory
cd /d "%INITIAL_DIR%"

exit /b 0

@REM Resolve the C cross compiler for the arm64 kernel. It needs aarch64-w64-mingw32-gcc from llvm-mingw.
@REM This used to be one machine's absolute install path, so the target built for nobody else.
:resolve_arm64_cc
if defined SIYUAN_ARM64_CC (
    set CC=%SIYUAN_ARM64_CC%
    goto :eof
)
where aarch64-w64-mingw32-gcc.exe >nul 2>&1
if errorlevel 1 (
    echo ERROR: aarch64-w64-mingw32-gcc.exe was not found.
    echo        Install llvm-mingw from https://github.com/mstorsjo/llvm-mingw/releases and add its bin
    echo        directory to PATH, or set SIYUAN_ARM64_CC to the full path of aarch64-w64-mingw32-gcc.exe.
    exit /b 1
)
set CC=aarch64-w64-mingw32-gcc.exe
goto :eof
