package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
)

// ---- ayudantes ----------------------------------------------------------

type llamadaPrecio struct {
	chain string
	url   string
}

// fetcherFalso es el PriceFetcher de los tests: guarda el orden en que le piden
// las fichas y devuelve lo que le digan, sin tocar la red.
type fetcherFalso struct {
	mu     sync.Mutex
	orden  []llamadaPrecio
	precio func(chainID, url string) chain.Product
	falla  func(chainID, url string) error
}

func (f *fetcherFalso) Fetch(_ context.Context, chainID, url string) (chain.Product, error) {
	f.mu.Lock()
	f.orden = append(f.orden, llamadaPrecio{chain: chainID, url: url})
	f.mu.Unlock()
	if f.falla != nil {
		if err := f.falla(chainID, url); err != nil {
			return chain.Product{}, err
		}
	}
	if f.precio != nil {
		return f.precio(chainID, url), nil
	}
	return chain.Product{Chain: chainID, URL: url, Price: 1.09, Available: true}, nil
}

func (f *fetcherFalso) llamadas() []llamadaPrecio {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llamadaPrecio(nil), f.orden...)
}

func (f *fetcherFalso) cadenas() []string {
	var out []string
	for _, c := range f.llamadas() {
		out = append(out, c.chain)
	}
	return out
}

// relojFalso solo avanza cuando el bucle duerme, así que se puede comprobar
// cuánto ha esperado sin esperar de verdad.
type relojFalso struct {
	mu       sync.Mutex
	ahora    time.Time
	dormidas []time.Duration
}

func nuevoReloj() *relojFalso {
	return &relojFalso{ahora: time.Now().UTC().Truncate(time.Second)}
}

func (r *relojFalso) now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ahora
}

func (r *relojFalso) dormir(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dormidas = append(r.dormidas, d)
	r.ahora = r.ahora.Add(d)
}

func (r *relojFalso) esperas() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.dormidas...)
}

// cadenaDe lee la cadena de la base, que es de donde salen ritmo y concurrencia.
func cadenaDe(t *testing.T, st *Store, id string) Chain {
	t.Helper()
	c, ok, err := st.Chain(id)
	if err != nil || !ok {
		t.Fatalf("Chain(%s): %v %v", id, ok, err)
	}
	return c
}

func bucleDePrueba(st *Store, f PriceFetcher, chains []Chain, r *relojFalso) *PriceLoop {
	l := NewPriceLoop(st, f, chains)
	l.Now = r.now
	l.Sleep = r.dormir
	return l
}

func TestElBucleRescataLoQueUnProcesoMuertoDejoAtascado(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	reloj := nuevoReloj()
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1", "m2"}})

	// m1 quedó a medias porque el proceso murió alengerarla, y m2 sigue pendiente.
	if _, err := st.db.Exec(`UPDATE price_queue SET state = 'descargando', updated_at = ?
		WHERE product_id = ?`, ts(time.Now().Add(-time.Hour)), productoID(t, st, "mercadona", "m1")); err != nil {
		t.Fatalf("atascar: %v", err)
	}
	f := &fetcherFalso{}
	loop := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, reloj)
	res, err := loop.Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 2 {
		t.Fatalf("hechos = %d, quiero 2: el bucle no rescató la atascada", res.Hechos)
	}
	for _, c := range f.cadenas() {
		if c == "" {
			t.Fatalf("llamadas = %+v", f.llamadas())
		}
	}
}

func TestUnFalloEsperaASuHora(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	reloj := nuevoReloj()
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	f := &fetcherFalso{falla: func(string, string) error { return errors.New("la web no responde") }}
	loop := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, reloj)

	res, err := loop.Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 0 || res.Fallidos != 1 {
		t.Fatalf("resumen = %+v", res)
	}

	id := productoID(t, st, "mercadona", "m1")
	estado, attempts, next, ultimo := estadoDe(t, st, id)
	if estado != estadoPendiente || attempts != 1 {
		t.Fatalf("cola = %q con %d intentos", estado, attempts)
	}
	if ultimo != "la web no responde" {
		t.Fatalf("last_error = %q", ultimo)
	}
	if quiero := ts(reloj.now().Add(backoffBase)); next != quiero {
		t.Fatalf("next_attempt_at = %s, quiero %s", next, quiero)
	}

	// Antes de su hora no sale.
	cola, err := st.NextQueuedChain(reloj.now(), "mercadona", 10)
	if err != nil || len(cola) != 0 {
		t.Fatalf("salió antes de su momento: %+v, %v", cola, err)
	}
	// A su hora vuelve a salir, con el intento acumulado y el error guardado.
	cola, err = st.NextQueuedChain(reloj.now().Add(backoffBase), "mercadona", 10)
	if err != nil || len(cola) != 1 {
		t.Fatalf("cola a su hora = %+v, %v", cola, err)
	}
	if cola[0].Attempts != 1 || cola[0].LastError != "la web no responde" {
		t.Fatalf("la reintentada perdió el intento o el error: %+v", cola[0])
	}
}

// El bucle no descarga una ficha que ya agotó sus intentos aunque alguien la
// dejara pendiente a mano: la aparca en 'error' sin golpear la tienda.
func TestElBucleNoTocaLoQueAgotoSusIntentos(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	id := productoID(t, st, "mercadona", "m1")
	if _, err := st.db.Exec(`UPDATE price_queue SET state = 'pendiente', attempts = ?, next_attempt_at = ?
		WHERE product_id = ?`, MaxIntentos, ts(time.Now().Add(-time.Hour)), id); err != nil {
		t.Fatalf("ajustar: %v", err)
	}

	f := &fetcherFalso{}
	loop := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, nuevoReloj())
	res, err := loop.Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 0 || res.Fallidos != 1 {
		t.Fatalf("resumen = %+v", res)
	}
	if len(f.llamadas()) != 0 {
		t.Fatalf("la gastó igual: %+v", f.llamadas())
	}
	if estado, _, _, _ := estadoDe(t, st, id); estado != estadoError {
		t.Fatalf("estado = %q, quiero %q", estado, estadoError)
	}
}

// ---- el bucle ------------------------------------------------------------

func TestElBucleRespetaLaPausaDeLaCadena(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	reloj := nuevoReloj()
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1", "m2", "m3"}})
	mercadona := cadenaDe(t, st, "mercadona")
	mercadona.PausaSegundos = 3
	mercadona.Concurrencia = 1

	res, err := bucleDePrueba(st, &fetcherFalso{}, []Chain{mercadona}, reloj).
		Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 3 {
		t.Fatalf("hechos = %d, quiero 3", res.Hechos)
	}

	// Tres descargas de la misma cadena, dos pausas: el test no espera de verdad,
	// solo mira lo que el bucle pidió dormir.
	esperas := reloj.esperas()
	if len(esperas) != 2 {
		t.Fatalf("esperas = %v, quiero dos pausas de 3s", esperas)
	}
	for _, d := range esperas {
		if d != 3*time.Second {
			t.Fatalf("espera = %s, quiero 3s", d)
		}
	}
}

// fetcherEnBarrena se queda esperando a que haya 'cuantos' fichas en vuelo: si
// el bucle no las pide en paralelo, no llega nunca y el test lo nota.
type fetcherEnBarrena struct {
	cuantos int
	mu      sync.Mutex
	enVuelo int
	llegó   chan struct{}
	libre   chan struct{}
}

func (f *fetcherEnBarrena) Fetch(_ context.Context, chainID, url string) (chain.Product, error) {
	f.mu.Lock()
	f.enVuelo++
	enVuelo := f.enVuelo
	f.mu.Unlock()
	if enVuelo == f.cuantos {
		close(f.llegó)
	}
	select {
	case <-f.libre:
	case <-time.After(3 * time.Second):
		return chain.Product{}, errors.New("no llegaron en paralelo")
	}
	f.mu.Lock()
	f.enVuelo--
	f.mu.Unlock()
	return chain.Product{Chain: chainID, URL: url, Price: 1, Available: true}, nil
}

func TestElBucleUsaLaConcurrenciaDeLaCadena(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1", "m2", "m3"}})
	mercadona := cadenaDe(t, st, "mercadona")
	mercadona.Concurrencia = 2
	mercadona.PausaSegundos = 0
	f := &fetcherEnBarrena{cuantos: 2, llegó: make(chan struct{}), libre: make(chan struct{})}
	loop := bucleDePrueba(st, f, []Chain{mercadona}, nuevoReloj())

	hechos := make(chan PriceJobResult, 1)
	go func() {
		res, _ := loop.Run(context.Background(), 10)
		hechos <- res
	}()
	// Con concurrencia 1 la segunda ficha no se pide nunca y esto revienta.
	select {
	case <-f.llegó:
	case <-time.After(3 * time.Second):
		t.Fatal("el bucle no pidió dos fichas a la vez con concurrencia 2")
	}
	close(f.libre)
	if res := <-hechos; res.Hechos != 3 || res.Fallidos != 0 {
		t.Fatalf("resumen = %+v", res)
	}
}

// fetcherEnPista se queda quieto hasta que hay 'cuantos' descargas a la vez y
// avisa por llegó. Un bucle que vaciara una cadena entera antes de pasar a la
// siguiente nunca llena la pista, así que el test lo nota. Cuando el test cierra
// parar, las demás llamadas pasan de largo.
type fetcherEnPista struct {
	cuantos  int
	llegaron chan struct{}
	parar    chan struct{}
	una      sync.Once

	mu       sync.Mutex
	enPista  int
	llamadas []llamadaPrecio
}

func (f *fetcherEnPista) Fetch(_ context.Context, chainID, url string) (chain.Product, error) {
	f.mu.Lock()
	f.enPista++
	f.llamadas = append(f.llamadas, llamadaPrecio{chain: chainID, url: url})
	pistas := f.enPista
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.enPista--
		f.mu.Unlock()
	}()
	if pistas >= f.cuantos {
		f.una.Do(func() { close(f.llegaron) })
	}
	select {
	case <-f.parar:
	case <-time.After(3 * time.Second):
		return chain.Product{}, errors.New("las cadenas no avanzan a la vez")
	}
	return chain.Product{Chain: chainID, URL: url, Price: 1, Available: true}, nil
}

func (f *fetcherEnPista) pedidas() []llamadaPrecio {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llamadaPrecio(nil), f.llamadas...)
}

func TestElBucleVaRoundRobin(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{
		"mercadona": {"m1", "m2", "m3"},
		"ahorramas": {"a1", "a2", "a3"},
		"dia":       {"d1", "d2", "d3"},
	})
	var orden []Chain
	for _, id := range []string{"mercadona", "ahorramas", "dia"} {
		c := cadenaDe(t, st, id)
		c.PausaSegundos = 0
		c.Concurrencia = 1
		orden = append(orden, c)
	}
	f := &fetcherEnPista{cuantos: 3, llegaron: make(chan struct{}), parar: make(chan struct{})}
	loop := bucleDePrueba(st, f, orden, nuevoReloj())
	hechos := make(chan PriceJobResult, 1)
	go func() {
		res, err := loop.Run(context.Background(), 10)
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		hechos <- res
	}()

	// Las tres cadenas tienen que tener su turno pedido a la vez: si el bucle
	// fuera cadena por cadena, esta espera se agota.
	select {
	case <-f.llegaron:
	case <-time.After(5 * time.Second):
		t.Fatal("el bucle no trabajó las tres cadenas a la vez")
	}
	close(f.parar)

	res := <-hechos
	if res.Hechos != 9 || res.Fallidos != 0 {
		t.Fatalf("resumen = %+v", res)
	}
	// Dentro de cada cadena el turno es el orden de la cola.
	porCadena := map[string][]string{}
	for _, c := range f.pedidas() {
		porCadena[c.chain] = append(porCadena[c.chain], c.url)
	}
	for chainID, quiero := range map[string][]string{
		"mercadona": {"m1", "m2", "m3"},
		"ahorramas": {"a1", "a2", "a3"},
		"dia":       {"d1", "d2", "d3"},
	} {
		got := porCadena[chainID]
		if len(got) != len(quiero) {
			t.Fatalf("%s = %v, quiero %v", chainID, got, quiero)
		}
		for i := range quiero {
			if got[i] != quiero[i] {
				t.Fatalf("%s = %v, quiero %v", chainID, got, quiero)
			}
		}
	}
}

func TestLasCadenasConLosPreciosApagadosNoSeTocan(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"alcampo": {"x1", "x2"}, "mercadona": {"m1"}})
	f := &fetcherFalso{}

	res, err := bucleDePrueba(st, f, cadenasCatalogo(), nuevoReloj()).Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 1 || res.Fallidos != 0 {
		t.Fatalf("resumen = %+v", res)
	}
	for _, c := range f.cadenas() {
		if c == "alcampo" {
			t.Fatalf("tocó alcampo, que está detrás de su WAF: %+v", f.llamadas())
		}
	}
	// Sus fichas se quedan esperando, que es lo único que puede pasar con ellas.
	stats, err := st.QueueStats("alcampo")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 2 {
		t.Fatalf("cola de alcampo = %+v", stats)
	}
}

func TestElBucleGuardaElPrecioYSacaLaFichaDeLaCola(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	f := &fetcherFalso{precio: func(chainID, url string) chain.Product {
		return chain.Product{Chain: chainID, URL: url, Price: 1.09,
			MeasurePrice: 1.09, MeasureUnit: "l", Available: true}
	}}

	res, err := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, nuevoReloj()).
		Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 1 || res.Fallidos != 0 {
		t.Fatalf("resumen = %+v", res)
	}

	p, ok, err := st.Product("mercadona", "m1")
	if err != nil || !ok {
		t.Fatalf("Product: %v %v", ok, err)
	}
	if p.Price != 1.09 || p.PriceBasis != "unidad" || p.MeasurePrice != 1.09 {
		t.Fatalf("precio = %+v", p)
	}
	if !p.Available || p.PriceFetchedAt.IsZero() {
		t.Fatalf("precio = %+v", p)
	}

	// Terminada la descarga, la ficha sale de la cola (MarkQueueDone la borra):
	// no queda ni pendiente ni descargando ni en error.
	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente]+stats[estadoDescargando]+stats[estadoError] != 0 {
		t.Fatalf("la ficha sigue en la cola: %+v", stats)
	}
	if cola, err := st.NextQueuedChain(time.Now().Add(time.Hour), "mercadona", 10); err != nil || len(cola) != 0 {
		t.Fatalf("vuelve a salir de la cola: %+v, %v", cola, err)
	}
}

// Un producto vendido al peso no trae precio de unidad (viene a 0 a propósito) y
// su único dato es el €/kg: se guarda igual y sale de la cola, no es un fallo.
func TestUnProductoAlPesoSeGuardaYNoSeReintenta(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	f := &fetcherFalso{precio: func(chainID, url string) chain.Product {
		return chain.Product{Chain: chainID, URL: url,
			MeasurePrice: 1.65, MeasureUnit: "kg", PriceIsPerMeasure: true, Available: true}
	}}

	res, err := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, nuevoReloj()).
		Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 1 || res.Fallidos != 0 {
		t.Fatalf("un producto al peso no es un fallo: %+v", res)
	}

	p, _, err := st.Product("mercadona", "m1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if p.Price != 0 || p.MeasurePrice != 1.65 || p.PriceBasis != "kg" {
		t.Fatalf("precio = %+v", p)
	}
	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente]+stats[estadoDescargando]+stats[estadoError] != 0 {
		t.Fatalf("la ficha sigue en la cola: %+v", stats)
	}
}

// El nombre y la categoría definitivos los pone catalog a través del enganche, no
// el bucle: aquí solo se comprueba que se llama.
func TestElBucleAvisaDeCadaFicha(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	f := &fetcherFalso{precio: func(chainID, url string) chain.Product {
		return chain.Product{Chain: chainID, URL: url, Price: 1.5, Available: true,
			Name: "Plátanos de Canarias", Category: "Fruta / Plátanos"}
	}}

	var vistas []string
	bucle := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, nuevoReloj())
	bucle.WithOnFicha(func(id int64, p chain.Product) {
		vistas = append(vistas, p.Name)
	})
	if _, err := bucle.Run(context.Background(), 10); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(vistas) != 1 || vistas[0] != "Plátanos de Canarias" {
		t.Fatalf("fichas vistas = %+v", vistas)
	}
}

// Una ficha que viene sin precio (o que ya no está) no se rellena con nada: se
// cuenta como fallida, sube los intentos y el producto se queda como estaba.
func TestUnaFichaSinPrecioNoSeRellena(t *testing.T) {
	for _, caso := range []struct {
		nombre   string
		producto chain.Product
		motivo   string
	}{
		{"sin precio", chain.Product{Price: 0, Available: true}, errSinPrecio.Error()},
		{"no disponible", chain.Product{Price: 1.5, Available: false}, errNoDisponible.Error()},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			st := abrir(t)
			preparar(t, st)
			sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
			mercadona := cadenaDe(t, st, "mercadona")
			f := &fetcherFalso{precio: func(chainID, url string) chain.Product {
				p := caso.producto
				p.Chain, p.URL = chainID, url
				return p
			}}

			res, err := bucleDePrueba(st, f, []Chain{mercadona}, nuevoReloj()).
				Run(context.Background(), 10)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Hechos != 0 || res.Fallidos != 1 {
				t.Fatalf("resumen = %+v", res)
			}

			p, _, err := st.Product("mercadona", "m1")
			if err != nil {
				t.Fatalf("Product: %v", err)
			}
			if p.Price != 0 || !p.PriceFetchedAt.IsZero() {
				t.Fatalf("inventó un precio: %+v", p)
			}
			estado, attempts, _, ultimo := estadoDe(t, st, productoID(t, st, "mercadona", "m1"))
			if estado != estadoPendiente || attempts != 1 || ultimo != caso.motivo {
				t.Fatalf("cola = %q, %d intentos, %q; quiero pendiente, 1, %q",
					estado, attempts, ultimo, caso.motivo)
			}
		})
	}
}

// Un 404 no se arregla reintentando: se aparca de una en 'error'.
func TestUnaFichaQueNoEstaNoSeReintenta(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	f := &fetcherFalso{falla: func(string, string) error { return chain.ErrNotFound }}

	res, err := bucleDePrueba(st, f, []Chain{cadenaDe(t, st, "mercadona")}, nuevoReloj()).
		Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Fallidos != 1 {
		t.Fatalf("resumen = %+v", res)
	}
	estado, attempts, _, _ := estadoDe(t, st, productoID(t, st, "mercadona", "m1"))
	if estado != estadoError || attempts != MaxIntentos {
		t.Fatalf("cola = %q con %d intentos, quiero %q con %d",
			estado, attempts, estadoError, MaxIntentos)
	}
}

func TestElBucleRespetaElLimitePorCadena(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1", "m2", "m3", "m4", "m5"}})
	mercadona := cadenaDe(t, st, "mercadona")
	mercadona.PausaSegundos = 0
	mercadona.Concurrencia = 1

	res, err := bucleDePrueba(st, &fetcherFalso{}, []Chain{mercadona}, nuevoReloj()).
		Run(context.Background(), 2)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Hechos != 2 {
		t.Fatalf("hechos = %d, quiero 2", res.Hechos)
	}
	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 3 {
		t.Fatalf("la pasada se comió la cola entera: %+v", stats)
	}
}

func TestElBucleRespetaElContexto(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})
	loop := bucleDePrueba(st, &fetcherFalso{}, []Chain{cadenaDe(t, st, "mercadona")}, nuevoReloj())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := loop.Run(ctx, 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, quiero %v", err, context.Canceled)
	}
	if res.Hechos != 0 {
		t.Fatalf("hechos = %d con el contexto cancelado", res.Hechos)
	}
	if _, err := loop.RunUntilEmpty(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunUntilEmpty = %v, quiero %v", err, context.Canceled)
	}
}

func TestRunUntilEmptyVaciaLaCola(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{
		"mercadona": {"m1", "m2", "m3"},
		"dia":       {"d1"},
	})
	chains := []Chain{cadenaDe(t, st, "mercadona"), cadenaDe(t, st, "dia")}
	for i := range chains {
		chains[i].PausaSegundos = 0
		chains[i].Concurrencia = 1
	}
	f := &fetcherFalso{}
	res, err := bucleDePrueba(st, f, chains, nuevoReloj()).RunUntilEmpty(context.Background())
	if err != nil {
		t.Fatalf("RunUntilEmpty: %v", err)
	}
	if res.Hechos != 4 || res.Fallidos != 0 {
		t.Fatalf("resumen = %+v", res)
	}
	stats, err := st.QueueStats("mercadona")
	if err != nil {
		t.Fatalf("QueueStats: %v", err)
	}
	if stats[estadoPendiente] != 0 {
		t.Fatalf("la cola no se vació: %+v", stats)
	}
	// Sin nada pendiente, una vuelta más no descarga nada ni se queda colgada.
	res, err = bucleDePrueba(st, f, chains, nuevoReloj()).RunUntilEmpty(context.Background())
	if err != nil || res.Hechos != 0 {
		t.Fatalf("segunda vuelta = %+v, %v", res, err)
	}
}

func TestPriceBasis(t *testing.T) {
	for _, caso := range []struct {
		nombre   string
		producto chain.Product
		quiero   string
	}{
		{"precio por unidad", chain.Product{Price: 1.09, MeasurePrice: 1.09, MeasureUnit: "l"}, "unidad"},
		// Con precio de unidad, la base es la unidad aunque también haya €/kg: el
		// precio que se guarda y se suma es el de la unidad.
		{"unidad y precio por kilo", chain.Product{Price: 2, MeasurePrice: 4, MeasureUnit: "kg"}, "unidad"},
		{"unidad y precio por litro", chain.Product{Price: 3.65, MeasurePrice: 4.87, MeasureUnit: "l"}, "unidad"},
		{"sin medida publicada", chain.Product{Price: 2}, "unidad"},
		// Al peso: Price está a 0 a propósito porque la tienda solo publica el
		// €/kg. Sin el flag, un 0 se leería como "precio de unidad".
		{"solo por kilo", chain.Product{Price: 0, MeasurePrice: 1.65, MeasureUnit: "kg",
			PriceIsPerMeasure: true}, "kg"},
		// Aunque el flag no venga, un precio 0 con medida publicada no puede
		// leerse como precio de unidad: los dos caminos coinciden.
		{"al peso sin el flag", chain.Product{Price: 0, MeasurePrice: 1.65, MeasureUnit: "kg"}, "kg"},
		{"sin nada", chain.Product{Price: 0}, "unidad"},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			if got := priceBasis(caso.producto); got != caso.quiero {
				t.Fatalf("priceBasis = %q, quiero %q", got, caso.quiero)
			}
		})
	}
}

func TestBackoffSubeHastaElTope(t *testing.T) {
	l := NewPriceLoop(nil, nil, nil)
	quiero := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second,
		240 * time.Second, 300 * time.Second, 300 * time.Second}
	for i, w := range quiero {
		if got := l.backoff(i + 1); got != w {
			t.Fatalf("backoff(%d) = %s, quiero %s", i+1, got, w)
		}
	}
	// Un número de intentos disparatado no desborda el tope.
	if got := l.backoff(200); got != backoffCap {
		t.Fatalf("backoff(200) = %s, quiero %s", got, backoffCap)
	}
}

// La unidad de la medida se guarda en el producto, no solo en el historial: sin
// ella la web no puede decir si el 1,88 es el pan entero o el kilo, y el DTO la
// lee de products.
func TestSetPriceGuardaLaUnidadDeLaMedida(t *testing.T) {
	st := abrir(t)
	preparar(t, st)
	sembrarPrecios(t, st, map[string][]string{"mercadona": {"m1"}})

	p, ok, err := st.Product("mercadona", "m1")
	if err != nil || !ok {
		t.Fatalf("Product: %v %v", ok, err)
	}
	if err := st.SetPrice(p.ID, 1.88, "unidad", 6.71, "kg", true); err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	p, _, err = st.Product("mercadona", "m1")
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if p.MeasureUnit != "kg" || p.MeasurePrice != 6.71 || p.Price != 1.88 {
		t.Fatalf("medida = %+v", p)
	}
}
