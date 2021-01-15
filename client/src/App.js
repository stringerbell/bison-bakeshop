import logo from "./logo.svg";
import "./App.css";
import PreOrder from "./Preorder";
import { BrowserRouter as Router, Route, Switch } from "react-router-dom";
import Success from "./Success";

function App() {
  return (
    <div className="App">
      <Router>
        <Switch>
          <Route path="/success">
            <Success />
          </Route>
          <Route path="/">
            <header className="App-header">
              <img src={logo} className="App-logo" alt="logo" />
            </header>
            <PreOrder />
          </Route>
        </Switch>
      </Router>
    </div>
  );
}

export default App;
