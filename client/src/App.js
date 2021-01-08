import logo from './logo.svg';
import './App.css';
import PreOrder from "./Preorder";

function App() {
  return (
    <div className="App">
      <header className="App-header">
        <img src={logo} className="App-logo" alt="logo" />
      </header>
      <PreOrder />
    </div>
  );
}

export default App;
