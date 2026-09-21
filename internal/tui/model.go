// Package tui implementa la interfaz de terminal con Bubble Tea v2.
// Solo conoce el núcleo (core.Core) y los eventos que este emite.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/miferdev/superComparator/internal/chain"
	"github.com/miferdev/superComparator/internal/config"
	"github.com/miferdev/superComparator/internal/core"
	"github.com/miferdev/superComparator/internal/list"
	"github.com/miferdev/superComparator/internal/report"
	"github.com/miferdev/superComparator/internal/store"
)

type screen int

const (
	screenPicker screen = iota
	screenList
	screenProgress
	screenCompare
	screenHistory
	screenReview
)

type (
	eventMsg struct{ ev core.Event }
	doneMsg  struct {
		phase string
		err   error
	}
	comparisonMsg struct {
		cmp core.Comparison
		err error
	}
	changesMsg struct {
		changes []store.Change
		err     error
	}
	alternativeMsg struct {
		item, chainID string
		product       chain.Product
		err           error
	}
)

type reviewOption struct {
	ChainID      string
	URL          string
	Name         string
	Score        float64
	Price        float64
	MeasurePrice float64
	MeasureUnit  string
}

type Model struct {
	cfg  config.Config
	core *core.Core
	log  *slog.Logger

	screen screen
	width  int
	height int

	browser browser
	spinner spinner.Model
	table   table.Model

	items        []list.Item
	status       map[string]string
	notes        map[string]string
	alts         map[string]map[string][]core.Alternative
	review       string
	options      []reviewOption
	reviewCursor int

	// scores y failed acumulan el resultado por cadena para derivar el estado
	// del producto sin depender del orden de los eventos.
	scores map[string]map[string]float64
	failed map[string]map[string]bool
	phase  string

	cmp        core.Comparison
	lineTotals map[string]float64
	grandTotal float64

	changes  []store.Change
	events   chan tea.Msg
	logLines []string
	running  bool
	message  string
}

// reviewThreshold es la similitud mínima para considerar un match fiable.
const reviewThreshold = 0.5

func New(cfg config.Config, c *core.Core, log *slog.Logger) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := Model{
		cfg:     cfg,
		core:    c,
		log:     log,
		screen:  screenPicker,
		browser: newBrowser(cfg.ListaDir),
		spinner: sp,
		table:   table.New(table.WithFocused(true), table.WithStyles(tableStyles())),
		status:  map[string]string{},
		notes:   map[string]string{},
		alts:    map[string]map[string][]core.Alternative{},
		scores:  map[string]map[string]float64{},
		failed:  map[string]map[string]bool{},

		lineTotals: map[string]float64{},
		events:     make(chan tea.Msg, 256),
	}
	if cfg.ListaPath != "" {
		if err := m.loadList(cfg.ListaPath); err != nil {
			m.message = err.Error()
		} else {
			m.screen = screenList
		}
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := keyOf(msg); ok {
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.screen == screenPicker {
				return m, tea.Quit
			}
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.browser.height = clamp(msg.Height-10, 5, 30)
		m.table.SetWidth(clamp(msg.Width-4, 40, 200))
		m.table.SetHeight(clamp(msg.Height-8, 5, 60))
		return m, nil
	case eventMsg:
		m.handleEvent(msg.ev)
		if m.screen == screenProgress {
			return m, waitForEvent(m.events)
		}
		return m, nil
	case doneMsg:
		m.running = false
		m.screen = screenList
		if msg.err != nil {
			m.message = "Error en " + msg.phase + ": " + msg.err.Error()
		} else {
			m.message = "Fase " + msg.phase + " terminada"
		}
		m.refreshItems()
		return m, nil
	case comparisonMsg:
		if len(msg.cmp.Items) == 0 && msg.err != nil {
			m.message = "Error al comparar: " + msg.err.Error()
			return m, nil
		}
		m.cmp = msg.cmp
		m.screen = screenCompare
		m.refreshCompare()
		if msg.err != nil {
			m.message = "No se pudo escribir el informe: " + msg.err.Error()
		}
		return m, nil
	case changesMsg:
		if msg.err != nil {
			m.message = "Error al leer el historial: " + msg.err.Error()
			return m, nil
		}
		m.changes = msg.changes
		m.screen = screenHistory
		m.refreshHistory()
		return m, nil
	case alternativeMsg:
		if msg.err != nil {
			m.message = "Error al elegir alternativa: " + msg.err.Error()
			return m, nil
		}
		m.status[msg.item] = "resuelto"
		m.notes[msg.item] = fmt.Sprintf("%s: %s %s", report.ChainName(msg.chainID), msg.product.Name, money(msg.product.Price))
		m.screen = screenList
		m.message = "Producto cambiado a " + msg.product.Name
		m.refreshItems()
		return m, nil
	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	switch m.screen {
	case screenPicker:
		if path, ok := m.browser.update(msg); ok {
			if err := m.loadList(path); err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.screen = screenList
			m.message = ""
		}
		return m, nil
	case screenList:
		if key, ok := keyOf(msg); ok {
			switch key {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "r":
				return m, m.startResolve()
			case "c":
				return m, m.compare()
			case "h":
				return m, m.history()
			case "enter":
				if len(m.items) == 0 || m.table.Cursor() >= len(m.items) {
					return m, nil
				}
				if len(m.buildReview(m.items[m.table.Cursor()].Name)) > 0 {
					m.review = m.items[m.table.Cursor()].Name
					m.options = m.buildReview(m.review)
					m.reviewCursor = 0
					m.screen = screenReview
					return m, nil
				}
			}
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	case screenProgress:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case screenReview:
		if key, ok := keyOf(msg); ok {
			switch {
			case key == "esc" || key == "q":
				m.screen = screenList
				return m, nil
			case key == "up" || key == "k":
				if m.reviewCursor > 0 {
					m.reviewCursor--
				}
			case key == "down" || key == "j":
				if m.reviewCursor < len(m.options)-1 {
					m.reviewCursor++
				}
			case key == "enter":
				if m.reviewCursor >= 0 && m.reviewCursor < len(m.options) {
					return m, m.chooseReview(m.reviewCursor)
				}
			default:
				if idx := keyNumber(key); idx >= 0 && idx < len(m.options) {
					m.reviewCursor = idx
					return m, m.chooseReview(idx)
				}
			}
		}
		return m, nil
	case screenCompare, screenHistory:
		if key, ok := keyOf(msg); ok && (key == "esc" || key == "q") {
			m.screen = screenList
			m.refreshItems()
			return m, nil
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) View() tea.View {
	content := m.view()
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "SuperComparator"
	return v
}

func (m *Model) loadList(path string) error {
	items, err := list.ParseFile(path)
	if err != nil {
		return err
	}
	if _, err := m.core.Store().SyncItems(items); err != nil {
		return err
	}
	m.items = items
	for _, it := range items {
		if _, ok := m.status[it.Name]; !ok {
			m.status[it.Name] = "pendiente"
		}
	}
	m.message = fmt.Sprintf("Lista cargada: %s (%d productos)", filepath.Base(path), len(items))
	m.refreshItems()
	return nil
}

func (m *Model) startResolve() tea.Cmd {
	m.running = true
	m.logLines = nil
	m.scores = map[string]map[string]float64{}
	m.failed = map[string]map[string]bool{}
	m.screen = screenProgress
	go func() {
		err := m.core.Resolve(context.Background(), m.items, func(e core.Event) {
			m.events <- eventMsg{e}
		})
		m.events <- doneMsg{phase: "resolver", err: err}
	}()
	return tea.Batch(m.spinner.Tick, waitForEvent(m.events))
}

func (m *Model) compare() tea.Cmd {
	m.message = "Calculando comparativa..."
	return func() tea.Msg {
		cmp, err := m.core.Comparison(context.Background())
		if err != nil {
			return comparisonMsg{cmp: cmp, err: err}
		}
		if len(cmp.Items) > 0 {
			if werr := report.Write(m.cfg.ReportPath, cmp); werr != nil {
				err = werr
			}
		}
		return comparisonMsg{cmp: cmp, err: err}
	}
}

func (m *Model) history() tea.Cmd {
	return func() tea.Msg {
		changes, err := m.core.Store().Changes(100)
		return changesMsg{changes: changes, err: err}
	}
}

func (m *Model) handleEvent(ev core.Event) {
	switch e := ev.(type) {
	case core.RunStarted:
		m.phase = e.Phase
	case core.ItemStarted:
		m.pushLine(fmt.Sprintf("[%d/%d] %s", e.Index, e.Total, e.Item))
	case core.ChainResolved:
		if m.scores[e.Item] == nil {
			m.scores[e.Item] = map[string]float64{}
		}
		m.scores[e.Item][e.Chain] = e.Score
		m.refreshStatus(e.Item)
		m.notes[e.Item] = fmt.Sprintf("%s: %s %s", report.ChainName(e.Chain), e.Product.Name, money(e.Product.Price))
		if m.alts[e.Item] == nil {
			m.alts[e.Item] = map[string][]core.Alternative{}
		}
		m.alts[e.Item][e.Chain] = e.Alternatives
		m.pushLine(fmt.Sprintf("  %s → %s (%s)", report.ChainName(e.Chain), e.Product.Name, money(e.Product.Price)))
	case core.ItemNeedsReview:
		m.refreshStatus(e.Item)
	case core.ItemFailed:
		if m.phase == "resolver" {
			if m.failed[e.Item] == nil {
				m.failed[e.Item] = map[string]bool{}
			}
			m.failed[e.Item][e.Chain] = true
			m.refreshStatus(e.Item)
		} else {
			m.status[e.Item] = "error"
		}
		m.notes[e.Item] = e.Chain + ": " + e.Err
		m.pushLine(fmt.Sprintf("  %s: %s", report.ChainName(e.Chain), e.Err))
	case core.PriceChanged:
		m.status[e.Item] = "cambio"
		m.notes[e.Item] = fmt.Sprintf("%s: %s → %s", report.ChainName(e.Chain), money(e.Old), money(e.New))
		m.pushLine(fmt.Sprintf("  %s: %s → %s", report.ChainName(e.Chain), money(e.Old), money(e.New)))
	case core.ProductDelisted:
		m.status[e.Item] = "descatalogado"
		m.notes[e.Item] = report.ChainName(e.Chain) + ": descatalogado"
	case core.RunFinished:
		m.pushLine(fmt.Sprintf("Fin: %d ok, %d revisar, %d fallos", e.Resolved, e.Review, e.Failed))
	}
}

// refreshStatus deriva el estado del producto a partir de las puntuaciones de
// todas las cadenas, para que un match flojo no quede oculto por el orden de
// llegada de los eventos.
func (m *Model) refreshStatus(item string) {
	scores := m.scores[item]
	if len(scores) > 0 {
		for _, score := range scores {
			if score < reviewThreshold {
				m.status[item] = "revisar"
				return
			}
		}
		if len(m.failed[item]) > 0 {
			m.status[item] = "revisar"
			return
		}
		m.status[item] = "resuelto"
		return
	}
	if len(m.failed[item]) > 0 {
		m.status[item] = "error"
		return
	}
	if _, ok := m.status[item]; !ok {
		m.status[item] = "pendiente"
	}
}

func (m *Model) buildReview(item string) []reviewOption {
	var opts []reviewOption
	for _, chainID := range m.core.Chains() {
		for _, alt := range m.alts[item][chainID] {
			opts = append(opts, reviewOption{
				ChainID: chainID, URL: alt.URL, Name: alt.Name, Score: alt.Score,
				Price: alt.Price, MeasurePrice: alt.MeasurePrice, MeasureUnit: alt.MeasureUnit,
			})
		}
	}
	return opts
}

func (m *Model) chooseReview(idx int) tea.Cmd {
	opt := m.options[idx]
	return func() tea.Msg {
		p, err := m.core.ChooseAlternative(context.Background(), m.review, opt.ChainID, opt.URL)
		return alternativeMsg{item: m.review, chainID: opt.ChainID, product: p, err: err}
	}
}

// reviewOptions adapta las alternativas a la regla de comparación del núcleo.
func (m *Model) reviewOptions() []core.ChainOption {
	opts := make([]core.ChainOption, len(m.options))
	for i, o := range m.options {
		opts[i] = core.ChainOption{
			Chain: o.ChainID, Product: o.Name, URL: o.URL, Price: o.Price,
			MeasurePrice: o.MeasurePrice, MeasureUnit: o.MeasureUnit, Available: true,
		}
	}
	return opts
}

func (m *Model) pushLine(line string) {
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > 12 {
		m.logLines = m.logLines[len(m.logLines)-12:]
	}
}

func waitForEvent(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}
