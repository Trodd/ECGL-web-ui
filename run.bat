@echo off
title ECGL League WebUI (Single Terminal)
echo =========================================
echo   🚀 Starting ECGL Frontend + Backend
echo =========================================

:: Move into backend folder and start Go API in background
cd backend
echo [BACKEND] Starting Go server on http://localhost:8080 ...
start /b go run . 

:: Move into frontend folder and start Vite React app
cd ../frontend
echo [FRONTEND] Starting React app on http://localhost:5173 ...
npm run dev

:: Go back to root when done
cd ..
