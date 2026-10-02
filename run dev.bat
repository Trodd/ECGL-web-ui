@echo off
title ECGL League WebUI (Single Terminal)
echo =========================================
echo   🚀 Starting ECGL Frontend + Backend
echo =========================================

:: Move into backend folder and start Go API in background
cd backend
echo [BACKEND] Stopping any previous instance...
taskkill /f /im ecgl-web.exe >nul 2>&1
echo [BACKEND] Building Go server to stable ecgl-web.exe...
go build -o ecgl-web.exe .
if errorlevel 1 (
    echo [BACKEND] Build failed - see errors above.
    pause
    exit /b 1
)
echo [BACKEND] Starting Go server (https://ecgleague.com)...
start /b ecgl-web.exe

:: Move into frontend folder and start Vite React app
cd ../frontend
echo [FRONTEND] Starting React app on http://localhost:5173 ...
npm run dev

:: Go back to root when done
cd ..
