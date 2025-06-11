# PDF duplicate code Scanner

Приложение для сканирования штрихкодов на страницах PDF-документов с графическим интерфейсом.

## 📦 Возможности

- Выбор PDF-файла
- Распознавание различных форматов штрихкодов (QR, Code128 и др.)
- Поиск дубликатов
- Графический интерфейс на Fyne
- Параллельная обработка страниц

---

## 🚀 Сборка (build)

Убедитесь, что у вас установлен [Go](https://golang.org/dl/) 1.18+ и Ghostscript:

### Установка зависимостей:

```bash
go mod tidy
```

### Сборка под текущую ОС:

```bash
go build -o scanner.exe barcode_scanner.go
```

Или под Linux/macOS:

```bash
GOOS=linux GOARCH=amd64 go build -o scanner barcode_scanner.go
```

---

## 🛠 Требования

### Обязательное ПО:
- **[Ghostscript](https://www.ghostscript.com/download/gsdnld.html)** — используется для конвертации PDF в PNG
  - На Windows: `gswin64c.exe` должен быть доступен в `%PATH%`
  - На Linux/macOS: команда `gs` должна быть в `$PATH`

> ❗ Программа покажет ошибку, если Ghostscript не установлен или не найден.

---

## 📥 Установка (для пользователя)

1. Установите Ghostscript
2. Скачайте или скопируйте `scanner.exe` (или `scanner`)
3. Запустите:
   ```bash
   ./scanner.exe      # Windows
   ./scanner          # Linux/macOS
   ```

---

## 🧪 Запуск без сборки

Можно запустить напрямую:
```bash
go run barcode_scanner.go
```

---

## 📁 Структура проекта

- `barcode_scanner.go` — основной код
- `go.mod / go.sum` — зависимости
- Используются библиотеки:
  - [fyne.io/fyne/v2](https://pkg.go.dev/fyne.io/fyne/v2) — GUI
  - `github.com/unidoc/unipdf/v3/model` — PDF-парсинг
  - `github.com/MarcoWel/gozxing` — штрихкоды

---

