import React from 'react';
import ReactDOM from 'react-dom/client';
import { Root } from './Root';
import '@radix-ui/themes/styles.css';
import './styles.css';
import './product.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
);
