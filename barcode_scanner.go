package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/MarcoWel/gozxing"
	"github.com/MarcoWel/gozxing/multi"
	"github.com/unidoc/unipdf/v3/model"
)

type BarcodeInfo struct {
	Pages []int
}

var (
	progressBar  *widget.ProgressBar
	statusLabel  *widget.Label
	resultsEntry *widget.Entry
	scanButton   *widget.Button
)

func main() {
	a := app.New()
	w := a.NewWindow("Сканер штрихкодов PDF")
	w.Resize(fyne.NewSize(800, 600))

	pdfPathEntry := widget.NewEntry()
	pdfPathEntry.SetPlaceHolder("Путь к PDF-файлу")

	selectFileButton := widget.NewButton("Выбрать файл PDF", func() {
		fileDialog := dialog.NewFileOpen(func(closer fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if closer == nil {
				return
			}
			pdfPathEntry.SetText(closer.URI().Path())
		}, w)

		// Установим размер окна проводника
		fileDialog.Resize(fyne.NewSize(800, 500))

		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".pdf"}))
		fileDialog.Show()
	})

	dpiEntry := widget.NewEntry()
	dpiEntry.SetText("200")
	dpiEntry.SetPlaceHolder("DPI (например, 200)")

	delayEntry := widget.NewEntry()
	delayEntry.SetText("150")
	delayEntry.SetPlaceHolder("Задержка (мс)")

	maxConcurrentEntry := widget.NewEntry()
	maxConcurrentEntry.SetText(strconv.Itoa(runtime.NumCPU() * 2))
	maxConcurrentEntry.SetPlaceHolder("Макс. параллельных операций")

	// Перечисление всех возможных форматов, которые gozxing может распознавать
	barcodeFormats := []string{
		"AZTEC", "CODABAR", "CODE_39", "CODE_93", "CODE_128", "DATA_MATRIX",
		"EAN_8", "EAN_13", "ITF", "MAXICODE", "PDF_417", "QR_CODE",
		"RSS_14", "RSS_EXPANDED", "UPC_A", "UPC_E", "UPC_EAN_EXTENSION",
	}

	selectedFormatsMap := make(map[gozxing.BarcodeFormat]bool)

	// Вспомогательная функция для конвертации строки в gozxing.BarcodeFormat
	stringToBarcodeFormat := func(s string) (gozxing.BarcodeFormat, bool) {
		switch s {
		case "AZTEC":
			return gozxing.BarcodeFormat_AZTEC, true
		case "CODABAR":
			return gozxing.BarcodeFormat_CODABAR, true
		case "CODE_39":
			return gozxing.BarcodeFormat_CODE_39, true
		case "CODE_93":
			return gozxing.BarcodeFormat_CODE_93, true
		case "CODE_128":
			return gozxing.BarcodeFormat_CODE_128, true
		case "DATA_MATRIX":
			return gozxing.BarcodeFormat_DATA_MATRIX, true
		case "EAN_8":
			return gozxing.BarcodeFormat_EAN_8, true
		case "EAN_13":
			return gozxing.BarcodeFormat_EAN_13, true
		case "ITF":
			return gozxing.BarcodeFormat_ITF, true
		case "MAXICODE":
			return gozxing.BarcodeFormat_MAXICODE, true
		case "PDF_417":
			return gozxing.BarcodeFormat_PDF_417, true
		case "QR_CODE":
			return gozxing.BarcodeFormat_QR_CODE, true
		case "RSS_14":
			return gozxing.BarcodeFormat_RSS_14, true
		case "RSS_EXPANDED":
			return gozxing.BarcodeFormat_RSS_EXPANDED, true
		case "UPC_A":
			return gozxing.BarcodeFormat_UPC_A, true
		case "UPC_E":
			return gozxing.BarcodeFormat_UPC_E, true
		case "UPC_EAN_EXTENSION":
			return gozxing.BarcodeFormat_UPC_EAN_EXTENSION, true
		default:
			return gozxing.BarcodeFormat(0), false
		}
	}

	// Установка значений по умолчанию (например, только CODE_128 и QR_CODE)
	initialSelectedStrings := []string{"CODE_128", "QR_CODE"}
	for _, s := range initialSelectedStrings {
		if format, ok := stringToBarcodeFormat(s); ok {
			selectedFormatsMap[format] = true
		}
	}

	barcodeFormatCheckGroup := widget.NewCheckGroup(barcodeFormats, func(selected []string) {
		// Очищаем предыдущие выборы
		for k := range selectedFormatsMap {
			delete(selectedFormatsMap, k)
		}
		// Обновляем на основе новых выборов
		for _, formatStr := range selected {
			if format, ok := stringToBarcodeFormat(formatStr); ok {
				selectedFormatsMap[format] = true
			}
		}
	})
	barcodeFormatCheckGroup.SetSelected(initialSelectedStrings)

	progressBar = widget.NewProgressBar()
	statusLabel = widget.NewLabel("Ожидание файла PDF...")
	resultsEntry = widget.NewMultiLineEntry()
	resultsEntry.SetMinRowsVisible(10)
	resultsEntry.Resize(fyne.NewSize(0, 200))
	resultsEntry.SetPlaceHolder("Результаты сканирования...")
	resultsEntry.Wrapping = fyne.TextWrapBreak

	scanButton = widget.NewButton("Начать сканирование", func() {
		pdfPath := pdfPathEntry.Text
		if pdfPath == "" {
			statusLabel.SetText("Ошибка: Пожалуйста, выберите файл PDF.")
			return
		}

		dpi, err := strconv.Atoi(dpiEntry.Text)
		if err != nil || dpi <= 0 {
			statusLabel.SetText("Ошибка: Неверное значение DPI.")
			return
		}

		delay, err := strconv.Atoi(delayEntry.Text)
		if err != nil || delay < 0 {
			statusLabel.SetText("Ошибка: Неверное значение задержки.")
			return
		}

		conc, err := strconv.Atoi(maxConcurrentEntry.Text)
		if err != nil || conc <= 0 {
			statusLabel.SetText("Ошибка: Неверное значение макс. параллельных операций.")
			return
		}

		// Конвертируем map selectedFormatsMap в срез для сканера
		formatsToScan := []gozxing.BarcodeFormat{}
		for format, isSelected := range selectedFormatsMap {
			if isSelected {
				formatsToScan = append(formatsToScan, format)
			}
		}
		if len(formatsToScan) == 0 {
			statusLabel.SetText("Ошибка: Пожалуйста, выберите хотя бы один формат штрихкода.")
			return
		}

		scanButton.Disable()
		progressBar.SetValue(0)
		resultsEntry.SetText("")
		statusLabel.SetText("Сканирование начато...")

		// Запускаем сканирование в отдельной горутине, чтобы не блокировать GUI
		go func() {
			scanPDF(pdfPath, dpi, time.Duration(delay)*time.Millisecond, formatsToScan, conc)
			scanButton.Enable()
		}()
	})

	inputForm := container.New(layout.NewFormLayout(),
		widget.NewLabel("Путь к PDF:"), pdfPathEntry,
		widget.NewLabel(""), selectFileButton, // Empty label to align button
		widget.NewLabel("DPI:"), dpiEntry,
		widget.NewLabel("Задержка (мс):"), delayEntry,
		widget.NewLabel("Макс. параллельных операций:"), maxConcurrentEntry,
	)

	// Оборачиваем CheckGroup в NewVScroll для адаптивной высоты
	scrollableBarcodeFormats := container.NewVScroll(barcodeFormatCheckGroup)
	// Fyne автоматически скорректирует высоту при добавлении содержимого.
	scrollableBarcodeFormats.SetMinSize(fyne.NewSize(0, 200))
	content := container.New(layout.NewVBoxLayout(),
		inputForm,
		widget.NewLabel("Форматы штрихкодов:"),
		scrollableBarcodeFormats,
		widget.NewSeparator(),
		scanButton,
		progressBar,
		statusLabel,
		widget.NewSeparator(),
		resultsEntry,
	)

	w.SetContent(content)
	w.ShowAndRun()
}

// scanPDF contains the core logic for PDF processing and barcode scanning
func scanPDF(pdfPath string, dpi int, delay time.Duration, formatsToScan []gozxing.BarcodeFormat, maxConcurrent int) {
	// Обработчик panic, чтобы предотвратить крах GUI при непредвиденных ошибках
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in scanPDF: %v", r)
			fyne.CurrentApp().SendNotification(&fyne.Notification{
				Title:   "Ошибка сканирования",
				Content: fmt.Sprintf("Произошла неожиданная ошибка: %v. Проверьте логи.", r),
			})
			statusLabel.SetText("Произошла неожиданная ошибка.")
		}
		scanButton.Enable()
	}()

	file, err := os.Open(pdfPath)
	if err != nil {
		fyne.CurrentApp().SendNotification(&fyne.Notification{
			Title:   "Ошибка файла",
			Content: fmt.Sprintf("Ошибка открытия PDF-файла: %v", err),
		})
		statusLabel.SetText(fmt.Sprintf("Ошибка: %v", err))
		return
	}
	defer file.Close()

	// Используем импортированный "model" из unipdf/v3/model
	pdfReader, err := model.NewPdfReader(file)
	if err != nil {
		fyne.CurrentApp().SendNotification(&fyne.Notification{
			Title:   "Ошибка PDF",
			Content: fmt.Sprintf("Ошибка чтения PDF-файла: %v", err),
		})
		statusLabel.SetText(fmt.Sprintf("Ошибка: %v", err))
		return
	}

	numPages, err := pdfReader.GetNumPages()
	if err != nil {
		fyne.CurrentApp().SendNotification(&fyne.Notification{
			Title:   "Ошибка PDF",
			Content: fmt.Sprintf("Ошибка получения количества страниц: %v", err),
		})
		statusLabel.SetText(fmt.Sprintf("Ошибка: %v", err))
		return
	}

	barcodeMap := make(map[string]*BarcodeInfo)
	var mu sync.Mutex
	var wg sync.WaitGroup

	tempDir, err := os.MkdirTemp("", "pdf_images")
	if err != nil {
		fyne.CurrentApp().SendNotification(&fyne.Notification{
			Title:   "Ошибка директории",
			Content: fmt.Sprintf("Не удалось создать временную директорию: %v", err),
		})
		statusLabel.SetText(fmt.Sprintf("Ошибка: %v", err))
		return
	}
	defer os.RemoveAll(tempDir)

	semaphore := make(chan struct{}, maxConcurrent) // Используем переданный maxConcurrent
	processedPages := 0

	// Обновление прогресса на главной горутине Fyne.
	go func() {
		for {
			select {
			case <-time.After(100 * time.Millisecond):
				currentProgress := float64(processedPages) / float64(numPages)
				progressBar.SetValue(currentProgress)
				statusLabel.SetText(fmt.Sprintf("Обработано %d из %d страниц...", processedPages, numPages))
				if processedPages >= numPages {
					return
				}
			}
		}
	}()

	for pageNum := 1; pageNum <= numPages; pageNum++ {
		wg.Add(1)
		go func(pn int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			log.Printf("Processing page %d...", pn)

			outputFilePattern := filepath.Join(tempDir, fmt.Sprintf("page-%d.png", pn))

			gsExecutable := "gs"
			if runtime.GOOS == "windows" {
				gsExecutable = "gswin64c.exe"
			}

			// Проверим, доступен ли Ghostscript в PATH
			gsPath, err := exec.LookPath(gsExecutable)
			if err != nil {
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "Ghostscript не найден",
					Content: fmt.Sprintf("Программа %s не найдена в PATH. Установите Ghostscript и проверьте переменные среды.", gsExecutable),
				})
				statusLabel.SetText(fmt.Sprintf("Ошибка: %s не найден. Установите Ghostscript.", gsExecutable))
				scanButton.Enable()
				return
			}

			cmdArgs := []string{
				"-dNOPAUSE",
				"-dBATCH",
				"-sDEVICE=png16m",
				fmt.Sprintf("-r%d", dpi),
				fmt.Sprintf("-dFirstPage=%d", pn),
				fmt.Sprintf("-dLastPage=%d", pn),
				fmt.Sprintf("-sOutputFile=%s", outputFilePattern),
				pdfPath,
			}

			imageFilename := outputFilePattern

			var gsSuccess = false
			for attempt := 1; attempt <= 3; attempt++ {
				cmd := exec.Command(gsPath, cmdArgs...)
				var stdoutBuf, stderrBuf bytes.Buffer
				cmd.Stdout = &stdoutBuf
				cmd.Stderr = &stderrBuf

				log.Printf("Running Ghostscript command for page %d (Attempt %d): %s %v", pn, attempt, cmd.Path, cmd.Args)

				cmdErr := cmd.Run()

				if cmdErr != nil {
					log.Printf("Attempt %d failed for page %d: %v\nStdout: %s\nStderr: %s",
						attempt, pn, cmdErr, stdoutBuf.String(), stderrBuf.String())
					break
				}

				time.Sleep(delay)

				if _, statErr := os.Stat(imageFilename); !os.IsNotExist(statErr) {
					gsSuccess = true
					break
				}
				log.Printf("Retry %d: Image file for page %d (%s) not found after Ghostscript execution. Stdout: %s, Stderr: %s",
					attempt, pn, imageFilename, stdoutBuf.String(), stderrBuf.String())
			}

			if !gsSuccess {
				log.Printf("ERROR: Image file for page %d (%s) does NOT exist after all retries. Skipping page.", pn, imageFilename)
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "Ошибка конвертации",
					Content: fmt.Sprintf("Не удалось конвертировать страницу %d. Файл не создан.", pn),
				})
				return
			}

			imgFile, err := os.Open(imageFilename)
			if err != nil {
				log.Printf("Error opening generated image file for page %d (%s): %v.", pn, imageFilename, err)
				return
			}
			defer imgFile.Close()

			img, _, err := image.Decode(imgFile)
			if err != nil {
				log.Printf("Error decoding image for page %d: %v. This might indicate a corrupt image or unreadable barcode.", pn, err)
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "Ошибка декодирования",
					Content: fmt.Sprintf("Не удалось декодировать изображение страницы %d.", pn),
				})
				return
			}

			barcodes, err := scanBarcodes([]image.Image{img}, formatsToScan)
			if err != nil {
				log.Printf("Error scanning barcodes on page %d: %v", pn, err)
				return
			}

			mu.Lock()
			for _, barcode := range barcodes {
				if info, exists := barcodeMap[barcode]; exists {
					info.Pages = append(info.Pages, pn)
				} else {
					barcodeMap[barcode] = &BarcodeInfo{Pages: []int{pn}}
				}
			}
			mu.Unlock()

			processedPages++

		}(pageNum)
	}

	wg.Wait()

	output := bytes.NewBufferString("Результаты сканирования штрихкодов:\n")
	foundDuplicates := false
	for barcode, info := range barcodeMap {
		if len(info.Pages) > 1 {
			fmt.Fprintf(output, "Дубликат штрихкода: %s (найден на страницах: %v)\n", barcode, info.Pages)
			foundDuplicates = true
		} else {
			fmt.Fprintf(output, "Уникальный штрихкод: %s (страница: %d)\n", barcode, info.Pages[0])
		}
	}

	if !foundDuplicates {
		output.WriteString("Дубликаты штрихкодов не найдены.\n")
	}
	log.Println("Barcode scan completed.")

	resultsEntry.SetText(output.String())
	statusLabel.SetText("Сканирование завершено.")
	progressBar.SetValue(1.0) // Ensure progress bar is full
}

// scanBarcodes сканирует штрихкоды на предоставленных изображениях, используя указанные форматы.
func scanBarcodes(images []image.Image, formats []gozxing.BarcodeFormat) ([]string, error) {
	var barcodes []string

	reader := multi.NewGenericMultipleBarcodeReader(multi.NewMultiFormatReader())

	hints := make(map[gozxing.DecodeHintType]interface{})
	if len(formats) > 0 {
		hints[gozxing.DecodeHintType_POSSIBLE_FORMATS] = formats
	}

	for _, img := range images {
		bmp, err := gozxing.NewBinaryBitmapFromImage(img)
		if err != nil {
			log.Printf("Error creating bitmap from image: %v", err)
			continue
		}

		results, err := reader.DecodeMultiple(bmp, hints)
		if err != nil {
			continue
		}

		for _, result := range results {
			barcodes = append(barcodes, result.GetText())
		}
	}

	return barcodes, nil
}
