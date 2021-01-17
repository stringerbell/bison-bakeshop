package main

import (
	_ "bison-bakeshop/memory"
	session2 "bison-bakeshop/session"
	"bison-bakeshop/util"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgconn"
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
	"mime"
	"mime/multipart"
	"net/http"
	netmail "net/mail"
	"os"
	"strconv"
	"time"
)

var conn *pgx.Conn
var mgr *session2.Manager

// initialize in init() function
func init() {
	globalSessions, err := session2.NewManager("memory", "sess", time.Hour*24*30)
	if err != nil {
		log.Fatal("error session2.NewManager", err)
	}
	mgr = globalSessions
	go mgr.GC()
}

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
	prefix := os.Getenv("API_PREFIX")

	http.Handle("/", http.FileServer(http.Dir(os.Getenv("STATIC_DIR"))))
	http.HandleFunc(prefix+"/pre-sales", handlePreSales)
	http.HandleFunc(prefix+"/config", handleConfig)
	http.HandleFunc(prefix+"/checkout-session", handleCheckoutSession)
	http.HandleFunc(prefix+"/create-checkout-session", handleCreateCheckoutSession)
	http.HandleFunc(prefix+"/webhook", handleWebhook)
	http.HandleFunc(prefix+"/incoming-email", handleIncomingEmail)
	http.HandleFunc(prefix+"/account", handleCreateAccount)
	http.HandleFunc(prefix+"/login", handleLogin)
	http.HandleFunc(prefix+"/logout", handleLogout)
	http.HandleFunc(prefix+"/session", handleCreateSession)
	http.HandleFunc(prefix+"/me", handleSessionRead)

	log.Println("server running at 0.0.0.0:4242")
	http.ListenAndServe("0.0.0.0:4242", nil)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	mgr.SessionDestroy(w, r)
	writeJSON(w, nil)
}

type identity struct {
	LoggedIn   bool   `json:"logged_in,omitempty"`
	CustomerID string `json:"customer_id,omitempty"`
}

const sessLoggedIn = "logged_in"
const sessCustomerID = "customer_id"
const sessCustomerEmail = "customer_email"

func handleSessionRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sess := mgr.SessionStart(w, r)
	loggedIn, _ := sess.Get(sessLoggedIn).(bool)
	customerID, _ := sess.Get(sessCustomerID).(string)
	writeJSON(w, identity{
		LoggedIn:   loggedIn,
		CustomerID: customerID,
	})
}

type sessionBody struct {
	Token string `json:"token"`
}
type sessionResponse struct {
	Location string `json:"location"`
}

func handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	j := sessionBody{}
	json.NewDecoder(r.Body).Decode(&j)
	if j.Token == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	bs, err := base64.URLEncoding.DecodeString(j.Token)
	if err != nil {
		writeJSON(w, sessionResponse{
			Location: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		})
		return
	}
	sess := mgr.SessionStart(w, r)

	if loggedIn, _ := sess.Get(sessLoggedIn).(bool); loggedIn {
		writeJSON(w, sessionResponse{
			Location: "/",
		})
		return
	}

	var loginTokenID *int64
	rows, err := conn.Query(context.Background(), `
  select login_tokens.id, accounts.customer, accounts.email
  from bakery.login_tokens
           join bakery.accounts on login_tokens.user_id = accounts.id
  where token = $1
    and login_tokens.created_at > now() - interval '10 minutes'
    and used_at is null
  `, string(bs))
	if err != nil {
		fmt.Printf("conn.QueryRow: %v", err)
		return
	}

	var customerID *string
	var customerEmail *string
	for rows.Next() {
		rows.Scan(&loginTokenID, &customerID, &customerEmail)
	}
	if loginTokenID == nil || *loginTokenID == 0 {
		writeJSON(w, sessionResponse{
			Location: "/",
		})
		return
	}

	_, err = conn.Exec(context.Background(), `
update bakery.login_tokens set used_at = now() where login_tokens.id = $1
`, loginTokenID)
	if err != nil {
		fmt.Printf("conn.Exec: %v", err)
		writeJSON(w, sessionResponse{
			Location: "/login",
		})
		return
	}
	sess.Set(sessLoggedIn, true)
	sess.Set(sessCustomerID, *customerID)
	sess.Set(sessCustomerEmail, *customerEmail)
	writeJSON(w, sessionResponse{
		Location: "/",
	})
}

type postBody struct {
	SessionID string `json:"sessionID,omitempty"`
	Email     string `json:"email,omitempty"`
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	j := postBody{}
	json.NewDecoder(r.Body).Decode(&j)
	if j.Email == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	go sendLoginLink(j.Email)
	writeJSON(w, j)
}

func sendLoginLink(email string) {
	var emailID *int64
	err := conn.QueryRow(context.Background(), "select id from bakery.accounts where email = $1", email).Scan(&emailID)
	if err != nil {
		fmt.Printf("conn.QueryRow: %v, email: %v\n", err, email)
		return
	}
	if emailID == nil {
		fmt.Printf("no email found to send to. Requested: %v\n", email)
		return
	}
	var unusedLogins *int
	conn.QueryRow(context.Background(), `
  select count(*)
  from bakery.login_tokens
  where user_id = $1
    and created_at > now() - interval '10 minutes'
    and used_at is null
`, emailID).Scan(&unusedLogins)
	if unusedLogins != nil && *unusedLogins != 0 {
		fmt.Printf("user (%s) who has unused login token is requesting a login\n", email)
		return
	}

	// we'll send a login link to their email
	m := mail.NewV3Mail()
	from := mail.NewEmail("Bison Bakeshop", os.Getenv("FROM_EMAIL"))
	m.SetReplyTo(mail.NewEmail("Dann", os.Getenv("REPLY_TO")))
	tokenLen, _ := strconv.Atoi(os.Getenv("TOKEN_LENGTH"))
	loginLink, err := util.GenerateRandomASCIIString(tokenLen)
	if err != nil {
		fmt.Printf("util.GenerateRandomASCIIString: %v", err)
		return
	}
	_, err = conn.Exec(context.Background(), "insert into bakery.login_tokens (token, user_id) values ($1,$2)", loginLink, emailID)
	if err != nil {
		fmt.Printf("conn.Exec: %v", err)
		return
	}
	content := mail.NewContent("text/plain", fmt.Sprintf(`
Hello!

A login has been requested for bison bakeshop. Please click the link below to login.

%s/login/%s

If you didn't request this, you can safely ignore this, and, I'd appreciate a heads up, so I can look into it.

Thank you!
Dann
`, os.Getenv("DOMAIN"), base64.URLEncoding.EncodeToString([]byte(loginLink))))

	m.SetFrom(from)
	m.AddContent(content)
	personalization := mail.NewPersonalization()
	personalization.AddTos(mail.NewEmail("", email))
	personalization.Subject = "Your login request"
	m.AddPersonalizations(personalization)
	client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
	if os.Getenv("NAME") == "prod" {
		re, err := client.Send(m)
		if err != nil {
			log.Printf("client.Send err: %v", err)
			return
		}
		log.Printf(strconv.Itoa(re.StatusCode))
	} else {
		fmt.Printf("would send email: %v", m.Content[0])
	}
}

func handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	j := postBody{}
	json.NewDecoder(r.Body).Decode(&j)
	if j.SessionID == "" || j.Email == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s, err := session.Get(j.SessionID, nil)
	if err != nil {
		fmt.Printf("session.Get: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if s == nil || s.Customer == nil {
		fmt.Printf("nil session or customer: %v", s)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, err = conn.Exec(context.Background(), `
  insert into bakery.accounts (customer, email) values ($1, $2)`,
		s.Customer.ID,
		j.Email,
	)
	if err != nil {
		fmt.Printf("conn.Query: %v", err)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				writeJSONErrorMessage(w, "would you like to login?", http.StatusConflict)
				return
			}
		}
		writeJSONErrorMessage(w, "unable to save your account", http.StatusInternalServerError)
		return
	}
	writeJSON(w, nil)
}

func handleIncomingEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	// make sure we have a key query param
	if len(q["key"]) < 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// make sure it matches ours
	if subtle.ConstantTimeCompare([]byte(q["key"][0]), []byte(os.Getenv("INCOMING_EMAIL_KEY"))) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	_, params, err := mime.ParseMediaType(r.Header.Get(http.CanonicalHeaderKey("content-type")))
	mr := multipart.NewReader(r.Body, params["boundary"])
	if err != nil {
		fmt.Printf("multipart.NewReader: %v", err)
		return
	}
	emailMap := map[string][]byte{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("mr.NextPart: %v", err)
			return
		}
		slurp, err := ioutil.ReadAll(p)
		if err != nil {
			fmt.Printf("ioutil.ReadAll: %v", err)
			return
		}
		_, ps, _ := mime.ParseMediaType(p.Header.Get(http.CanonicalHeaderKey("content-disposition")))
		emailMap[ps["name"]] = slurp
	}

	go forwardEmail(emailMap)
	writeJSON(w, "200 👍🏻")
}

func forwardEmail(emailMap map[string][]byte) {
	m := mail.NewV3Mail()
	a, err := netmail.ParseAddress(string(emailMap["from"]))
	if err != nil {
		log.Printf("netmail.ParseAddress: %v", err)
		return
	}
	from := mail.NewEmail(a.Name, a.Address)
	m.SetFrom(mail.NewEmail("Dann", os.Getenv("FROM_EMAIL")))
	m.SetReplyTo(from)

	r := bytes.NewReader(emailMap["email"])
	parsed, err := netmail.ReadMessage(r)
	if err != nil {
		log.Printf("netmail.ReadMessage: %v", err)
		return
	}
	_, params, err := mime.ParseMediaType(parsed.Header.Get(http.CanonicalHeaderKey("content-type")))
	if err != nil {
		fmt.Printf("multipart.NewReader: %v", err)
		return
	}
	mr := multipart.NewReader(parsed.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("mr.NextPart: %v", err)
			return
		}
		slurp, err := ioutil.ReadAll(p)
		if err != nil {
			fmt.Printf("ioutil.ReadAll: %v", err)
			return
		}
		mt, _, _ := mime.ParseMediaType(p.Header.Get(http.CanonicalHeaderKey("content-type")))
		m.AddContent(mail.NewContent(mt, string(slurp)))
	}
	personalization := mail.NewPersonalization()
	personalization.Subject = string(emailMap["subject"])
	personalization.AddTos(mail.NewEmail("Dann", os.Getenv("FORWARD_TO_EMAIL")))
	m.AddPersonalizations(personalization)
	client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
	re, err := client.Send(m)
	if err != nil {
		log.Printf("client.Send err: %v", err)
		return
	}
	log.Printf(strconv.Itoa(re.StatusCode))
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
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	rs, err := conn.Query(context.Background(), `
    select presales.id,
           coalesce(sum(qty), 0) as sold,
           presales.total_qty,
           to_char(presales.date, 'Dy, Mon DD, YYYY')
    from bakery.orders
             right join bakery.presales on presale_id = presales.id
    where presales.deleted = false
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
	s, err := session.Get(sessionID, nil)
	if err != nil {
	  var sError *stripe.Error
    if errors.As(err, &sError) {
      writeJSONErrorMessage(w, sError.Msg, sError.HTTPStatusCode)
      return
    }
    writeJSONErrorMessage(w, err.Error(), http.StatusBadRequest)
    return
  }
	var c *stripe.Customer
	var email string
	if s.Customer != nil {
		c, _ = customer.Get(s.Customer.ID, nil)
		if c != nil {
			email = c.Email
		}
	}
	s.CustomerEmail = email
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
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sess := mgr.SessionStart(w, r)
	req := checkoutSessionCreateReq{}
	json.NewDecoder(r.Body).Decode(&req)
	customerID, _ := sess.Get(sessCustomerID).(string)
	customerEmail, _ := sess.Get(sessCustomerEmail).(string)
	if customerID != "" {
		err := createReservation(req, customerEmail, customerID)
		if err != nil {
			writeJSONErrorMessage(w, fmt.Sprintf("can't create pre-order: %v", err), http.StatusInternalServerError)
		} else {
			go sendPreorderEmail(preorderEmail{
				qty:           int(req.Quantity),
				date:          req.Date.Format("2006-01-02"),
				price:         float64(priceForQuantity(req.Quantity)),
				customerEmail: customerEmail,
			})
			writeJSON(w, "👍🏻")
		}
		return
	}
	domainURL := os.Getenv("DOMAIN")
	cParams := &stripe.CustomerParams{}
	c, err := customer.New(cParams)
	if err != nil {
		writeJSONErrorMessage(w, fmt.Sprintf("can't create customer: %v", err), http.StatusInternalServerError)
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
		SuccessURL: stripe.String(domainURL + "/success?s={CHECKOUT_SESSION_ID}"),
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

type preorderEmail struct {
	qty           int
	date          string // 2020-01-01
	price         float64
	customerEmail string
}

func sendPreorderEmail(e preorderEmail) {
	layout := "Mon, Jan _2, 2006"
	date, _ := time.Parse("2006-01-02", e.date)
	m := mail.NewV3Mail()
	from := mail.NewEmail("Bison Bakeshop", os.Getenv("FROM_EMAIL"))
	m.SetReplyTo(mail.NewEmail("Dann", os.Getenv("REPLY_TO")))
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
`, date.Format(layout),
		e.qty,
		e.price/100,
		os.Getenv("PICKUP_ADDRESS"),
		os.Getenv("SUPPORT_PHONE"),
	),
	)
	m.SetFrom(from)
	m.AddContent(content)
	personalization := mail.NewPersonalization()
	personalization.AddTos(mail.NewEmail("", e.customerEmail))
	personalization.AddBCCs(mail.NewEmail("", os.Getenv("BCC_EMAIL")))
	personalization.Subject = "Your cinnamon roll pre-order for " + date.Format(layout)
	m.AddPersonalizations(personalization)
	client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
	if os.Getenv("NAME") == "prod" {
		re, err := client.Send(m)
		if err != nil {
			log.Printf("client.Send err: %v", err)
			return
		}
		log.Printf(strconv.Itoa(re.StatusCode))
	} else {
		fmt.Printf("would send email: %v", m.Content[0])
	}
}

func createReservation(req checkoutSessionCreateReq, customerEmail, customerID string) error {
	t, err := conn.Exec(context.Background(),
		`
      insert into bakery.orders (email, presale_id, qty, price, customer_id, session_id)
      values ($1, $2, $3, $4, $5, $6)
    `,
		customerEmail, req.ID, req.Quantity, strconv.FormatInt(priceForQuantity(req.Quantity), 10), customerID, nil,
	)
	fmt.Println(t)
	return err
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
	if r.Method != http.MethodPost {
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
		c, err := customer.Get(sesh.Customer.ID, nil)
		if err != nil {
			log.Printf("customer.Get %v", err)
			return
		}
		qty, _ := strconv.Atoi(sesh.Metadata["qty"])
		totalPrice, _ := strconv.ParseFloat(sesh.Metadata["price"], 64)
		go sendPreorderEmail(preorderEmail{
			qty:           qty,
			date:          sesh.Metadata["requested_date"],
			price:         totalPrice,
			customerEmail: c.Email,
		})
	}
	writeJSON(w, event)
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
	conn.Exec(context.Background(),
		`
      insert into bakery.orders (email, presale_id, qty, price, customer_id, session_id)
      values ($1, $2, $3, $4, $5, $6)
    `,
		c.Email, id, qty, p, c.ID, event.ID,
	)
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
