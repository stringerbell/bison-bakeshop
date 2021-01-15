import React, { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import "./App.css";
import RenderIf from "./RenderIf";
import logo from "./logo.svg";
import "./css/success.css";

export default function Success() {
  const [form, setForm] = useState({
    error: "",
    success: "",
    loading: false,
  });
  const [email, setEmail] = useState("");
  const location = useLocation();
  const sessionID = location.search.replace("?s=", "");
  useEffect(() => {
    async function fetchSession() {
      const res = await fetch(
        "/checkout-session?sessionId=" + sessionID
      ).then((res) => res.json());
      // setSession(res);
      setEmail(res.customer_email);
    }

    fetchSession();
  }, [sessionID]);

  const createAccount = async () => {
    setForm({ ...form, loading: true });
    await fetch("/account", {
      method: "POST",
      body: JSON.stringify({ sessionId: sessionID, email }),
    })
      .then((res) => {
        if (res.ok) {
          return res.json();
        }
        throw new Error(res.status);
      })
      .then((json) => {
        setForm({
          ...form,
          error: "",
          success: "All set! Thank you!",
        });
      })
      .catch((e) => {
        switch (e.message) {
          case "409":
            setForm({
              ...form,
              success: "",
              error: "You already have an account with us.",
            });
            break;
          case "400":
            setForm({
              ...form,
              success: "",
              error: "stop trying to hack me, plz",
            });
            break;
          default:
            setForm({ ...form, success: "", error: "something went wrong 😭" });
            break;
        }
      });
  };
  return (
    <div className={'success-wrapper-outer'}>
      {/*<header className="App-header">*/}
      {/*<img src={logo} className="App-logo" alt="logo" />*/}

      <p>Thank you for your order!</p>
      <p>Check your email for pickup instructions.</p>
      <div className={"success-wrapper"}>
        <div className={'md-vw success-wrapper-inner'}>
          <p>Create an account for faster reservations in the future?</p>
          <input
            className={'md-vw'}
            disabled={form.success}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <RenderIf condition={form.error} fallback={null}>
            <h1 className={'md-vw'}>{form.error}</h1>
          </RenderIf>
          <RenderIf condition={form.success} fallback={null}>
            <h1 className={'md-vw'}>{form.success}</h1>
          </RenderIf>
          <button
            className={"button md-vw"}
            disabled={form.loading || form.success}
            onClick={createAccount}
          >
            Create Account
          </button>
        </div>
      </div>
      {/*</header>*/}
    </div>
  );
}
