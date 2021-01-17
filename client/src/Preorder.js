import React, { useEffect, useReducer, useState } from "react";
import Modal from "react-modal";
import Checkout from "./Checkout";
import "./css/preorder.css";
import "./App.css";
import { Link } from "react-router-dom";
import logo from "./logo.svg";
import RenderIf from "./RenderIf";

function reducer(state, action) {
  switch (action.type) {
    case "useEffectUpdate":
      return action.payload;
    default:
      return [];
  }
}

export default function PreOrder() {
  const [id, setID] = useState({});
  const [modalIsOpen, setIsOpen] = useState(false);
  const [selected, setSelected] = useState({});
  const [dates, dispatch] = useReducer(reducer, []);

  useEffect(() => {
    async function fetchPreSales() {
      // Fetch dates/availability from our backend.
      const dates = await fetch("/api/pre-sales").then((res) => res.json());
      dispatch({
        type: "useEffectUpdate",
        payload: dates,
      });
    }

    fetchPreSales();
  }, []);

  useEffect(() => {
    async function fetchID() {
      const response = await fetch("/api/me").then((r) => r.json());
      setID(response);
    }
    fetchID();
  }, []);

  const qty = (available) => {
    if (available === 0) {
      return "Sold Out!";
    }
    if (available <= 6) {
      return `Only ${available} left!`;
    }
    return `${available} available`;
  };

  const onClick = (event, { date, id, available }) => {
    setSelected({ date, available, id });
    setIsOpen(true);
  };
  const closeModal = () => {
    setSelected({});
    setIsOpen(false);
  };
  Modal.setAppElement("#root");

  return (
    <>
      <RenderIf
        condition={!id.logged_in}
        fallback={
          <Link className={"login-link"} to={"/logout"}>
            <button>Logout</button>
          </Link>
        }
      >
        <Link className={"login-link"} to={"/login"}>
          <button>Login</button>
        </Link>
      </RenderIf>
      <header className="App-header">
        <img src={logo} className="App-logo" alt="logo" />
      </header>
      <div className={"pre-order-container"}>
        <p className={"p"}>Pre-order cinnamon rolls:</p>
        <ul className={"pre-order-list"}>
          {dates.map(({ available, date, id }) => (
            <div key={id}>
              <button
                disabled={available <= 0}
                onClick={(e) => onClick(e, { date, id, available })}
                className={"pre-order-date"}
              >
                {date}
              </button>
              <span className={"qty"}>{qty(available)}</span>
            </div>
          ))}
        </ul>
        <Modal
          isOpen={modalIsOpen}
          onRequestClose={closeModal}
          contentLabel="Pre-Order A Roll"
        >
          <div onClick={closeModal} className={"close-modal-btn"}>
            ✖
          </div>
          <Checkout selected={selected} />
        </Modal>
      </div>
    </>
  );
}
