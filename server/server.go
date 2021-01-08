package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v4"
	"github.com/joho/godotenv"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"github.com/stripe/stripe-go/v71"
	"github.com/stripe/stripe-go/v71/checkout/session"
	"github.com/stripe/stripe-go/v71/customer"
	"github.com/stripe/stripe-go/v71/price"
	"github.com/stripe/stripe-go/v71/webhook"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var conn *pgx.Conn

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	checkEnv()

	conn, err = pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("error pgx.Connect", err)
	}

	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")

	http.Handle("/", http.FileServer(http.Dir(os.Getenv("STATIC_DIR"))))
	http.HandleFunc("/pre-sales", handlePreSales)
	http.HandleFunc("/config", handleConfig)
	http.HandleFunc("/checkout-session", handleCheckoutSession)
	http.HandleFunc("/create-checkout-session", handleCreateCheckoutSession)
	http.HandleFunc("/webhook", handleWebhook)

	log.Println("server running at 0.0.0.0:4242")
	http.ListenAndServe("0.0.0.0:4242", nil)
}

// ErrorResponseMessage represents the structure of the error
// object sent in failed responses.
type ErrorResponseMessage struct {
	Message string `json:"message"`
}

// ErrorResponse represents the structure of the error object sent
// in failed responses.
type ErrorResponse struct {
	Error *ErrorResponseMessage `json:"error"`
}

type preSale struct {
	ID        int `json:"id"`
	Sold      int
	Total     int
	Available int    `json:"available"`
	Date      string `json:"date"`
}

func handlePreSales(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rs, err := conn.Query(context.Background(), `
    select presales.id,
           coalesce(sum(qty), 0) as sold,
           presales.total_qty,
           to_char(presales.date, 'Dy, Mon DD, YYYY')
    from bakery.orders
             right join bakery.presales on presale_id = presales.id
    group by bakery.orders.presale_id, presales.id
    order by presale_id`,
		)
		if err != nil {
			log.Fatal("conn.Query", err)
		}
		pss := []preSale{}
		for rs.Next() {
			var ps preSale
			err := rs.Scan(&ps.ID, &ps.Sold, &ps.Total, &ps.Date)
			if err != nil {
				log.Fatal("rs.Scan", err)
			}
			ps.Available = ps.Total - ps.Sold // ugh
			pss = append(pss, ps)
		}
		if rs.Err() != nil {
			log.Fatal("rs.Err", rs.Err())
		}
		writeJSON(w, pss)
		return
	}
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {

		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	// Fetch a price, use its unit amount and currency
	p, _ := price.Get(
		os.Getenv("PRICE"),
		nil,
	)
	dp, _ := price.Get(
		os.Getenv("BULK_PRICE"),
		nil,
	)
	writeJSON(w, struct {
		PublicKey      string `json:"publicKey"`
		UnitAmount     int64  `json:"unitAmount"`
		DiscountAmount int64  `json:"discountAmount"`
		Currency       string `json:"currency"`
	}{
		PublicKey:      os.Getenv("STRIPE_PUBLISHABLE_KEY"),
		UnitAmount:     p.UnitAmount,
		DiscountAmount: dp.UnitAmount,
		Currency:       string(p.Currency),
	})
}

func handleCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	sessionID := r.URL.Query().Get("sessionId")
	fmt.Println(sessionID)
	s, _ := session.Get(sessionID, nil)
	writeJSON(w, s)
}

type checkoutSessionCreateReq struct {
	Quantity    int64     `json:"quantity"`
	Reservation bool      `json:"reservation"`
	ID          int64     `json:"id"`
	Price       int64     `json:"price"`
	Date        time.Time `json:"date"`
}

func handleCreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	req := checkoutSessionCreateReq{}
	json.NewDecoder(r.Body).Decode(&req)
	domainURL := os.Getenv("DOMAIN")
	cParams := &stripe.CustomerParams{}
	c, err := customer.New(cParams)
	if err != nil {
		writeJSONError(w, fmt.Errorf("can't create customer: %w", err), http.StatusInternalServerError)
		return
	}

	// Create new Checkout Session for the order
	// Other optional params include:
	// [billing_address_collection] - to display billing address details on the page
	// [customer] - if you have an existing Stripe Customer ID
	// [payment_intent_data] - lets capture the payment later
	// [customer_email] - lets you prefill the email input in the form
	// For full details see https://stripe.com/docs/api/checkout/sessions/create

	// ?session_id={CHECKOUT_SESSION_ID} means the redirect will have the session ID
	// set as a query param
	params := &stripe.CheckoutSessionParams{
		Params: stripe.Params{
			Metadata: map[string]string{
				"id":             strconv.FormatInt(req.ID, 10),
				"qty":            strconv.FormatInt(req.Quantity, 10),
				"price":          strconv.FormatInt(priceForQuantity(req.Quantity), 10),
				"requested_date": req.Date.Format("2006-01-02"),
			},
		},
		Customer:  stripe.String(c.ID),
		CancelURL: stripe.String(domainURL),
		Mode:      stripe.String(string(stripe.CheckoutSessionModeSetup)),
		PaymentMethodTypes: stripe.StringSlice([]string{
			"card",
		}),
		SuccessURL: stripe.String(domainURL),
	}
	s, err := session.New(params)
	if err != nil {
		http.Error(w, fmt.Sprintf("error while creating session %v", err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, struct {
		SessionID string `json:"sessionId"`
	}{
		SessionID: s.ID,
	})
}
func priceForQuantity(q int64) int64 {
	// Fetch a price, use its unit amount and currency
	p, _ := price.Get(
		os.Getenv("PRICE"),
		nil,
	)
	bp, _ := price.Get(
		os.Getenv("BULK_PRICE"),
		nil,
	)
	total := p.UnitAmount * q
	if q > 3 {
		total = bp.UnitAmount * q
	}
	return total
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Printf("ioutil.ReadAll: %v", err)
		return
	}

	event, err := webhook.ConstructEvent(b, r.Header.Get("Stripe-Signature"), os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Printf("webhook.ConstructEvent: %v", err)
		return
	}

	if event.Type == "checkout.session.completed" {
		fmt.Println("Checkout Session completed!")
		var sesh stripe.CheckoutSession
		_ = json.Unmarshal(event.Data.Raw, &sesh)
		go addPreorder(sesh)
		go sendEmail(sesh)
	}
	writeJSON(w, event)
}

func sendEmail(event stripe.CheckoutSession) {
	if event.Customer == nil {
		log.Printf("nil customer %v", event.ID)
		return
	}
	c, err := customer.Get(event.Customer.ID, nil)
	if err != nil {
		log.Printf("customer.Get %v", err)
		return
	}
	qty := event.Metadata["qty"]
	d := event.Metadata["requested_date"]
	layout := "Mon, Jan _2, 2006"
	date, _ := time.Parse("2006-01-02", d)
	p, _ := strconv.ParseFloat(event.Metadata["price"], 64)

	m := mail.NewV3Mail()
	from := mail.NewEmail("Bison Bakeshop", os.Getenv("FROM_EMAIL"))
	m.SetReplyTo(mail.NewEmail("Dann Stockton", os.Getenv("FROM_EMAIL")))
	content := mail.NewContent("text/plain", fmt.Sprintf(`
Thanks for your pre-order!

Details:
  - Pickup date: %v (Rolls are out of the oven at 8am)
  - Quantity: %v
  - Total Price: $%.2f

My address is:
%s

If you have any questions, respond to this email, or feel free to call/text me: %s

Thank you!
Dann
`, date.Format(layout), qty, p/100, os.Getenv("PICKUP_ADDRESS"), os.Getenv("SUPPORT_PHONE")),
	)
	m.SetFrom(from)
	m.AddContent(content)
	personalization := mail.NewPersonalization()
	personalization.AddTos(mail.NewEmail("", c.Email))
	personalization.AddBCCs(mail.NewEmail("", os.Getenv("BCC_EMAIL")))
	personalization.Subject = "Your cinnamon roll pre-order for " + date.Format(layout)
	m.AddPersonalizations(personalization)
	client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
	re, err := client.Send(m)
	if err != nil {
		log.Printf("client.Send err: %v", err)
		return
	}
	log.Printf(strconv.Itoa(re.StatusCode))
}

func addPreorder(event stripe.CheckoutSession) {
	if event.Customer == nil {
		log.Printf("nil customer %v", event.ID)
		return
	}
	c, err := customer.Get(event.Customer.ID, nil)
	if err != nil {
		log.Printf("customer.Get %v", err)
		return
	}
	id := event.Metadata["id"]
	qty := event.Metadata["qty"]
	p := event.Metadata["price"]
	ct, err := conn.Exec(context.Background(),
		`
      insert into bakery.orders (email, presale_id, qty, price, customer_id, session_id)
      values ($1, $2, $3, $4, $5, $6)
    `,
		c.Email, id, qty, p, c.ID, event.ID,
	)

	fmt.Println(c, event, ct, "err", err)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("json.NewEncoder.Encode: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := io.Copy(w, &buf); err != nil {
		log.Printf("io.Copy: %v", err)
		return
	}
}

func writeJSONError(w http.ResponseWriter, v interface{}, code int) {
	w.WriteHeader(code)
	writeJSON(w, v)
	return
}

func writeJSONErrorMessage(w http.ResponseWriter, message string, code int) {
	resp := &ErrorResponse{
		Error: &ErrorResponseMessage{
			Message: message,
		},
	}
	writeJSONError(w, resp, code)
}

func checkEnv() {
	p := os.Getenv("PRICE")
	if p == "price_12345" || p == "" {
		log.Fatal("You must set a Price ID from your Stripe account. See the README for instructions.")
	}
}
