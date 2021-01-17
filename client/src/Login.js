import "./css/login.css";
import { useEffect, useState } from "react";
import RenderIf from "./RenderIf";
import {Link, useLocation} from "react-router-dom";
import logo from "./logo.svg";

export default function Login() {
  let {search} = useLocation();
  const [email, setEmail] = useState(search.replace("?email=", ""));
  const [complete, setComplete] = useState(false);
  useEffect(() => {
    fetch("/me")
      .then((r) => r.json())
      .then((j) => {
        if (j.logged_in) {
          window.location.href = "/";
        }
      });
  }, []);
  const onClick = (e) => {
    e.preventDefault();
    fetch("/login", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ email }),
    })
      .then((res) => res.json())
      .then((j) => setComplete(true));
  };
  return (
    <div className={"login-wrapper"}>
      <Link to={'/'}><img src={logo} className="App-logo" alt="logo" /></Link>
      <RenderIf
        condition={!complete}
        fallback={<h1>Check your email, and click the link to login.</h1>}
      >
        <h1>Login with your email</h1>
        <div className={"login-inner-wrapper"}>
          <form onSubmit={onClick}>
            <input
              type={"email"}
              required={true}
              placeholder={"email"}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <button type={"sumbit"}>Login</button>
          </form>
        </div>
      </RenderIf>
    </div>
  );
}
