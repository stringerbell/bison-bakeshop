import React from "react";
import PreOrder from "./Preorder";
import { BrowserRouter as Router, Link, Route, Switch } from "react-router-dom";
import Success from "./Success";
import Login from "./Login";
import LoginRequest from "./LoginRequest";
import Logout from "./Logout";

function App() {
  return (
    <div className="App">
      <Router>
        <Switch>
          <Route exact path="/">
            <PreOrder />
          </Route>
          <Route exact path={"/login/:token"}>
            <LoginRequest />
          </Route>
          <Route exact path={"/login"}>
            <Login />
          </Route>
          <Route exact path={"/logout"}>
            <Logout />
          </Route>
          <Route exact path="/success">
            <Success />
          </Route>
        </Switch>
      </Router>
    </div>
  );
}

export default App;
