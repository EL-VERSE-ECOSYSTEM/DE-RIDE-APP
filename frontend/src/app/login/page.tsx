'use client';
import {useState} from 'react';
export default function Login(){const [email,setEmail]=useState('');return <main style={{maxWidth:420,margin:'80px auto',padding:24}}><a href="/">← DE RIDE</a><h1>Sign in</h1><form onSubmit={e=>{e.preventDefault();alert('Connect the API service to enable sign in.')}}><label>Email<input required type="email" value={email} onChange={e=>setEmail(e.target.value)} style={{display:'block',width:'100%',padding:14,margin:'8px 0 16px',background:'#243036',color:'white'}} /></label><button type="submit">Continue with email</button></form></main>}
