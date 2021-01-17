import React, { useState, useEffect } from "react";
import { Redirect, useParams } from "react-router-dom";

export default function LoginRequest() {
  let { token } = useParams();
  const [location, setLocation] = useState(null);
  useEffect(() => {
    async function loginRequest() {
      await fetch("/api/session", {
        method: "POST",
        body: JSON.stringify({ token }),
      })
        .then((res) => res.json())
        .then((j) => {
          setLocation(j.location);
        });
    }

    if (token !== undefined) {
      loginRequest();
    }
  }, []);
  return location && <Redirect to={location} />;
}
