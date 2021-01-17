import React, {useEffect} from "react";
import { Redirect } from "react-router-dom";
import { useState } from "react";

export default function Logout() {
  const [complete, setComplete] = useState(false);
  useEffect(() => {
    async function logout() {
      // Fetch config from our backend.
      await fetch("/logout")
        .then((res) => res.json())
        .then(() => setComplete(true));
    }

    logout();
  }, []);

  return complete && <Redirect to={"/"} />;
}
