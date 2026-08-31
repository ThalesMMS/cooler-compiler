@REM SPDX-License-Identifier: AGPL-3.0-or-later
@echo off
REM Cooler Compiler Lab - Build and Run Script for Windows
REM This is a batch wrapper for the PowerShell script

if "%1"=="" goto help
if "%1"=="test" goto test
if "%1"=="vet" goto vet
if "%1"=="fmt" goto fmt
if "%1"=="check" goto check
if "%1"=="build" goto build
if "%1"=="package" goto package
if "%1"=="package-native" goto package-native
if "%1"=="run" goto run
if "%1"=="browser" goto browser
if "%1"=="clean" goto clean
if "%1"=="help" goto help

echo Unknown command: %1
goto help

:test
powershell -ExecutionPolicy Bypass -File build.ps1 test
goto end

:vet
powershell -ExecutionPolicy Bypass -File build.ps1 vet
goto end

:fmt
powershell -ExecutionPolicy Bypass -File build.ps1 fmt
goto end

:check
powershell -ExecutionPolicy Bypass -File build.ps1 check
goto end

:build
powershell -ExecutionPolicy Bypass -File build.ps1 build
goto end

:package
powershell -ExecutionPolicy Bypass -File build.ps1 package
goto end

:package-native
powershell -ExecutionPolicy Bypass -File build.ps1 package-native
goto end

:run
powershell -ExecutionPolicy Bypass -File build.ps1 run
goto end

:browser
powershell -ExecutionPolicy Bypass -File build.ps1 browser
goto end

:clean
powershell -ExecutionPolicy Bypass -File build.ps1 clean
goto end

:help
echo Cooler Compiler Lab - Build and Run Script for Windows
echo.
echo Usage: build.bat [command]
echo.
echo Commands:
echo   test           - Run tests
echo   vet            - Run go vet
echo   fmt            - Format Go code
echo   check          - Run tests, vet, and check JavaScript
echo   build          - Build coolergui and coolerc binaries
echo   package        - Create standalone executable package (browser mode)
echo   package-native - Create standalone executable with native window support
echo   run            - Run the GUI application
echo   browser        - Run the GUI in browser mode
echo   clean          - Remove build artifacts
echo   help           - Show this help message
echo.
echo Examples:
echo   build.bat build
echo   build.bat run
echo   build.bat check
goto end

:end
