import React, {useEffect, useReducer} from 'react';
import {loadStripe} from '@stripe/stripe-js';
import rolls from './img/rolls.jpg';
import RenderIf from "./RenderIf";
import OutOfStock from "./OutOfStock";

const fetchCheckoutSession = async ({quantity, date}) => {
  return fetch('/create-checkout-session', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      quantity,
      date
    }),
  }).then((res) => res.json());
};

const formatPrice = ({amount, discountAmount, currency, quantity}) => {
  const numberFormat = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency,
    currencyDisplay: 'symbol',
  });
  const parts = numberFormat.formatToParts(amount);
  let zeroDecimalCurrency = true;
  for (let part of parts) {
    if (part.type === 'decimal') {
      zeroDecimalCurrency = false;
    }
  }
  amount = zeroDecimalCurrency ? amount : amount / 100;
  discountAmount = zeroDecimalCurrency ? discountAmount : discountAmount / 100;
  let total = (amount * quantity);
  if (quantity > 3) {
    total = quantity * discountAmount;
  }

  return numberFormat.format(total.toFixed(2));
};

function reducer(state, action) {
  switch (action.type) {
    case 'useEffectUpdate':
      return {
        ...state,
        ...action.payload,
        price: formatPrice({
          amount: action.payload.unitAmount,
          discountAmount: action.payload.discountAmount,
          currency: action.payload.currency,
          quantity: state.quantity,
        }),
      };
    case 'increment':
      return {
        ...state,
        quantity: state.quantity + 1,
        price: formatPrice({
          amount: state.unitAmount,
          discountAmount: state.discountAmount,
          currency: state.currency,
          quantity: state.quantity + 1,
        }),
      };
    case 'decrement':
      return {
        ...state,
        quantity: state.quantity - 1,
        price: formatPrice({
          amount: state.unitAmount,
          discountAmount: state.discountAmount,
          currency: state.currency,
          quantity: state.quantity - 1,
        }),
      };
    case 'setLoading':
      return {...state, loading: action.payload.loading};
    case 'setError':
      return {...state, error: action.payload.error};
    default:
      throw new Error();
  }
}

const Checkout = ({selected: {date, available}}) => {
  const [state, dispatch] = useReducer(reducer, {
    quantity: 1,
    price: null,
    discountAmount: null,
    loading: false,
    error: null,
    stripe: null,
  });

  useEffect(() => {
    async function fetchConfig() {
      // Fetch config from our backend.
      const {publicKey, unitAmount, discountAmount, currency} = await fetch(
        '/config'
      ).then((res) => res.json());
      // Make sure to call `loadStripe` outside of a component’s render to avoid
      // recreating the `Stripe` object on every render.
      dispatch({
        type: 'useEffectUpdate',
        payload: {unitAmount, discountAmount, currency, stripe: await loadStripe(publicKey)},
      });
    }

    fetchConfig();
  }, []);

  const handleClick = async (event) => {
    // Call your backend to create the Checkout session.
    dispatch({type: 'setLoading', payload: {loading: true}});
    const {sessionId} = await fetchCheckoutSession({
      quantity: state.quantity,
      date: (new Date(date)).toISOString(),
    });
    // When the customer clicks on the button, redirect them to Checkout.
    const {error} = await state.stripe.redirectToCheckout({
      sessionId,
    });
    // If `redirectToCheckout` fails due to a browser or network
    // error, display the localized error message to your customer
    // using `error.message`.
    if (error) {
      dispatch({type: 'setError', payload: {error}});
      dispatch({type: 'setLoading', payload: {loading: false}});
    }
  };

  return (
    <RenderIf condition={available !== 0} fallback={<OutOfStock/>}>
      <div className="sr-root">
        <div className="sr-main">
          <section className="container">
            <div>
              <h1>Cinnamon Roll</h1>
              <h4>Pre-order a cinnamon roll for {date}</h4>
              <div className="pasha-image">
                <img
                  alt="Cinnamon Rolls"
                  src={rolls}
                  width="280"
                  height="320"
                />
              </div>
            </div>
            <div className="quantity-setter">
              <button
                className="increment-btn"
                disabled={state.quantity === 1}
                onClick={() => dispatch({type: 'decrement'})}
              >
                -
              </button>
              <input
                type="number"
                id="quantity-input"
                min="1"
                max={available}
                value={state.quantity}
                readOnly
              />
              <button
                className="increment-btn"
                disabled={state.quantity === Math.min(available, 12)}
                onClick={() => dispatch({type: 'increment'})}
              >
                +
              </button>
            </div>
            <p className="sr-legal-text">Number of rolls (max {Math.min(available, 12)})</p>

            <button
              role="link"
              onClick={handleClick}
              disabled={!state.stripe || state.loading}
            >
              {state.loading || !state.price
                ? `Loading...`
                : `Buy for ${state.price}`}
            </button>
            <div className="sr-field-error">{state.error?.message}</div>
          </section>
        </div>
      </div>
    </RenderIf>
  );
};

export default Checkout;
