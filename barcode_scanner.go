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
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

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

// ──────────────────────────────── helpers ────────────────────────────────

type BarcodeInfo struct{ Pages []int }

func sanitize(s string) string { // нормализуем код для сравнения
	s = strings.TrimSpace(s)
	return strings.Map(func(r rune) rune {
		if r < 32 { // управляющие удалить
			return -1
		}
		return unicode.ToUpper(r)
	}, s)
}

// runOnUI выполняет fn в главном UI‑потоке Fyne.
func runOnUI(fn func()) {
	fyne.Do(fn)
}

// ───────────────────────────── GUI ─────────────────────────────

var (
	progressBar  *widget.ProgressBar
	statusLabel  *widget.Label
	resultsEntry *widget.Entry
	scanButton   *widget.Button
)

func main() {
	a := app.New()
	w := a.NewWindow("Сканер штрихкодов PDF")
	w.Resize(fyne.NewSize(820, 640))

	// поля ввода
	pdfPathEntry := widget.NewEntry()
	pdfPathEntry.SetPlaceHolder("Путь к PDF‑файлу")

	chooseBtn := widget.NewButton("Выбрать…", func() {
		fd := dialog.NewFileOpen(func(f fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if f == nil {
				return
			}
			pdfPathEntry.SetText(f.URI().Path())
		}, w)
		fd.Resize(fyne.NewSize(800, 500))
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".pdf"}))
		fd.Show()
	})

	dpiEntry := widget.NewEntry()
	dpiEntry.SetText("160")
	delayEntry := widget.NewEntry()
	delayEntry.SetText("100")
	concEntry := widget.NewEntry()
	concEntry.SetText(strconv.Itoa(runtime.NumCPU()))

	// спискок поддерживаемых форматов
	formats := []string{"AZTEC", "CODABAR", "CODE_39", "CODE_93", "CODE_128", "DATA_MATRIX", "EAN_8", "EAN_13", "ITF", "MAXICODE", "PDF_417", "QR_CODE", "RSS_14", "RSS_EXPANDED", "UPC_A", "UPC_E", "UPC_EAN_EXTENSION"}
	selFmt := map[gozxing.BarcodeFormat]bool{}

	toFmt := func(s string) (gozxing.BarcodeFormat, bool) {
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
		}
		return 0, false
	}

	initFmt := []string{"CODE_128", "QR_CODE"}
	for _, s := range initFmt {
		if f, ok := toFmt(s); ok {
			selFmt[f] = true
		}
	}

	check := widget.NewCheckGroup(formats, func(selected []string) {
		for k := range selFmt {
			delete(selFmt, k)
		}
		for _, s := range selected {
			if f, ok := toFmt(s); ok {
				selFmt[f] = true
			}
		}
	})
	check.SetSelected(initFmt)

	// элементы состояния
	progressBar = widget.NewProgressBar()
	statusLabel = widget.NewLabel("Ожидание файла PDF...")
	resultsEntry = widget.NewMultiLineEntry()
	resultsEntry.SetMinRowsVisible(10)
	resultsEntry.Resize(fyne.NewSize(0, 200))
	resultsEntry.SetPlaceHolder("Результаты сканирования...")
	resultsEntry.Wrapping = fyne.TextWrapBreak

	// кнопка Сканировать
	scanButton = widget.NewButton("Сканировать", func() {
		pdfPath := pdfPathEntry.Text
		if pdfPath == "" {
			statusLabel.SetText("Ошибка: Пожалуйста, выберите файл PDF.")
			return
		}
		dpi, err := strconv.Atoi(dpiEntry.Text)
		if err != nil || dpi <= 0 {
			statusLabel.SetText("Некорректный DPI")
			return
		}
		delay, err := strconv.Atoi(delayEntry.Text)
		if err != nil || delay < 0 {
			statusLabel.SetText("Некорректная задержка")
			return
		}
		conc, err := strconv.Atoi(concEntry.Text)
		if err != nil || conc <= 0 {
			statusLabel.SetText("Некорректный параллелизм")
			return
		}
		var fmts []gozxing.BarcodeFormat
		for f := range selFmt {
			fmts = append(fmts, f)
		}
		if len(fmts) == 0 {
			statusLabel.SetText("Выберите формат")
			return
		}

		scanButton.Disable()
		progressBar.SetValue(0)
		resultsEntry.SetText("")
		statusLabel.SetText("Сканирование…")

		go func() {
			scanPDF(pdfPathEntry.Text, dpi, time.Duration(delay)*time.Millisecond, fmts, conc)
			runOnUI(scanButton.Enable)
		}()
	})

	// раскладка
	form := container.New(layout.NewFormLayout(),
		widget.NewLabel("PDF:"), pdfPathEntry,
		widget.NewLabel(""), chooseBtn,
		widget.NewLabel("DPI:"), dpiEntry,
		widget.NewLabel("Задержка (мс):"), delayEntry,
		widget.NewLabel("Потоков:"), concEntry,
	)
	scrollableBarcodeFormats := container.NewVScroll(check)

	scrollableBarcodeFormats.SetMinSize(fyne.NewSize(0, 250))
	ui := container.New(layout.NewVBoxLayout(),
		form,
		widget.NewLabel("Форматы:"), scrollableBarcodeFormats,
		widget.NewSeparator(),
		scanButton, progressBar, statusLabel, widget.NewSeparator(), resultsEntry,
	)
	w.SetContent(ui)
	w.ShowAndRun()
}

// ─────────────────────────── core ────────────────────────────

func scanPDF(pdfPath string, dpi int, delay time.Duration, fmts []gozxing.BarcodeFormat, conc int) {
	startTime := time.Now()
	file, err := os.Open(pdfPath)
	if err != nil {
		runOnUI(func() { statusLabel.SetText(fmt.Sprintf("Ошибка открытия: %v", err)) })
		return
	}
	defer file.Close()

	reader, err := model.NewPdfReader(file)
	if err != nil {
		runOnUI(func() { statusLabel.SetText(fmt.Sprintf("Ошибка чтения PDF: %v", err)) })
		return
	}
	numPages, err := reader.GetNumPages()
	if err != nil {
		runOnUI(func() { statusLabel.SetText(fmt.Sprintf("Ошибка страниц: %v", err)) })
		return
	}

	// ограничить параллелизм максимумом ядер
	if conc > runtime.NumCPU() {
		conc = runtime.NumCPU()
	}
	sem := make(chan struct{}, conc)

	tempDir, err := os.MkdirTemp("", "pdfimg")
	if err != nil {
		runOnUI(func() { statusLabel.SetText(fmt.Sprintf("tmp dir: %v", err)) })
		return
	}
	defer os.RemoveAll(tempDir)

	codeMap := make(map[string]*BarcodeInfo)
	var mu sync.Mutex
	var processed int64

	// прогресс‑бар в отдельной горутине, обновление через UI‑поток
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			cur := atomic.LoadInt64(&processed)
			runOnUI(func() {
				progressBar.SetValue(float64(cur) / float64(numPages))
				statusLabel.SetText(fmt.Sprintf("Обработано %d из %d страниц...", cur, numPages))
			})
			if cur >= int64(numPages) {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for p := 1; p <= numPages; p++ {
		wg.Add(1)
		go func(page int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			imgPath := filepath.Join(tempDir, fmt.Sprintf("page-%d.png", page))
			gs := "gs"
			if runtime.GOOS == "windows" {
				gs = "gswin64c.exe"
			}
			exe, err := exec.LookPath(gs)
			if err != nil {
				log.Printf("Ghostscript: %v", err)
				return
			}
			args := []string{"-dNOPAUSE", "-dBATCH", "-sDEVICE=pnggray", fmt.Sprintf("-r%d", dpi), fmt.Sprintf("-dFirstPage=%d", page), fmt.Sprintf("-dLastPage=%d", page), fmt.Sprintf("-sOutputFile=%s", imgPath), pdfPath}
			for i := 0; i < 3; i++ {
				if err = exec.Command(exe, args...).Run(); err == nil {
					if _, err2 := os.Stat(imgPath); err2 == nil {
						break
					}
				}
				time.Sleep(400 * time.Millisecond)
			}
			imgFile, err := os.Open(imgPath)
			if err != nil {
				return
			}
			defer imgFile.Close()
			img, _, err := image.Decode(imgFile)
			if err != nil {
				return
			}
			codes, _ := scanBarcodes([]image.Image{img}, fmts)

			mu.Lock()
			for _, c := range codes {
				key := sanitize(c)
				if bi, ok := codeMap[key]; ok {
					bi.Pages = append(bi.Pages, page)
				} else {
					codeMap[key] = &BarcodeInfo{Pages: []int{page}}
				}
			}
			mu.Unlock()
			atomic.AddInt64(&processed, 1)
			time.Sleep(delay)
		}(p)
	}
	wg.Wait()

	var buf bytes.Buffer
	buf.WriteString("Результаты:\n")
	dup := false
	for k, v := range codeMap {
		if len(v.Pages) > 1 {
			dup = true
			fmt.Fprintf(&buf, "Дубликат %s на стр. %v\n", k, v.Pages)
		}
	}
	if !dup {
		buf.WriteString("Дубликатов не найдено.\n")
	}

	runOnUI(func() {
		resultsEntry.SetText(buf.String())
		duration := time.Since(startTime)
		statusLabel.SetText(fmt.Sprintf("Готово за %v", duration.Truncate(time.Millisecond)))
		progressBar.SetValue(1)
	})
}

// ───────────────────────── barcodes ──────────────────────────

func scanBarcodes(imgs []image.Image, fmts []gozxing.BarcodeFormat) ([]string, error) {
	rdr := multi.NewGenericMultipleBarcodeReader(multi.NewMultiFormatReader())
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	if len(fmts) > 0 {
		hints[gozxing.DecodeHintType_POSSIBLE_FORMATS] = fmts
	}
	var out []string
	for _, img := range imgs {
		bmp, err := gozxing.NewBinaryBitmapFromImage(img)
		if err != nil {
			continue
		}
		res, err := rdr.DecodeMultiple(bmp, hints)
		if err != nil {
			continue
		}
		for _, r := range res {
			out = append(out, r.GetText())
		}
	}
	return out, nil
}
