import React from "react";
import Modal from 'react-modal';
import Checkout from "./Checkout";
import "./css/preorder.css";

export default function PreOrder() {
  const [modalIsOpen, setIsOpen] = React.useState(false);
  const [selected, setSelected] = React.useState({})

  const dates = [
    {date: 'Sunday, January 3, 2021', available: 0},
    {date: 'Sunday, January 10, 2021', available: 3},
    {date: 'Sunday, January 17, 2021', available: 24},
    {date: 'Sunday, January 24, 2021', available: 24},
    {date: 'Sunday, January 31, 2021', available: 24}
  ]

  const qty = available => {
    if (available === 0) {
      return 'Sold Out!'
    }
    if (available <= 6) {
      return `Only ${available} left!`;
    }
    return `${available} available`;
  }

  const onClick = (event, date, available) => {
    setSelected({date, available})
    setIsOpen(true)
  }
  const closeModal = () => {
    setSelected({})
    setIsOpen(false)
  }
  Modal.setAppElement('#root');

  return (
    <div className={'pre-order-container'}>
      <p className={'p'}>Pre-order cinnamon rolls:</p>
      <ul className={'pre-order-list'}>
        {
          dates.map(({available, date}) => (
            <div key={date}>
              <button disabled={available === 0} onClick={(e) => onClick(e, date, available)} className={'pre-order-date'}>{date}</button>
              <span className={'qty'}>{qty(available)}</span>
            </div>
          ))
        }
      </ul>
      <Modal
        isOpen={modalIsOpen}
        onRequestClose={closeModal}
        contentLabel="Pre-Order A Roll"
      >
        <div onClick={closeModal} className={'close-modal-btn'}>✖</div>
        <Checkout selected={selected}/>
      </Modal>
    </div>
  );
}
