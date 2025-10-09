@echo off
cd /d C:\ECGLwebui\frontend

echo 🗑️ Cleaning old build...
rmdir /s /q dist
rmdir /s /q node_modules

echo 📦 Installing dependencies...
npm install

echo 🏗️ Building frontend...
npm run build

echo ✅ Build complete! Dist folder is at:
echo    C:\ECGLwebui\frontend\dist

echo 🚀 Done!
pause
