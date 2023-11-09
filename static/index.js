const box = document.getElementById('upload');
const uploadButton = document.getElementById('upload-label');
box.onmouseenter = box.ondragenter = box.onfocus = box.onclick = () => {
	uploadButton.classList.add('active');
};
box.onmouseleave = box.ondragleave = box.onblur = box.ondrop = () => {
	uploadButton.classList.remove('active');
};
box.onchange = async () => {
	for (const file of box.files) {
		const form = new FormData();
		form.append('file', file)

		const postRes = await fetch('/files', {
			method: 'POST',
			headers: { Authorization: auth.currentUser.accessToken },
			body: form,
		});

		const fileData = await postRes.json();

		renderFileView([fileData, ...allFiles]);
	}
};

let allFiles;

const renderFileView = files => {
	allFiles = files;

	const parent = document.getElementById('files');
	parent.innerHTML = '';

	for (const file of files) {
		parent.innerHTML += `
		<div>
			<a href="/files/${file.id}" class="name" target="_blank">${file.name}</a>
			<svg onclick="deleteFile('${file.id}')" fill="currentColor" class="delete" width="24" height="24" clip-rule="evenodd" fill-rule="evenodd" stroke-linejoin="round" stroke-miterlimit="2" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="m4.015 5.494h-.253c-.413 0-.747-.335-.747-.747s.334-.747.747-.747h5.253v-1c0-.535.474-1 1-1h4c.526 0 1 .465 1 1v1h5.254c.412 0 .746.335.746.747s-.334.747-.746.747h-.254v15.435c0 .591-.448 1.071-1 1.071-2.873 0-11.127 0-14 0-.552 0-1-.48-1-1.071zm14.5 0h-13v15.006h13zm-4.25 2.506c-.414 0-.75.336-.75.75v8.5c0 .414.336.75.75.75s.75-.336.75-.75v-8.5c0-.414-.336-.75-.75-.75zm-4.5 0c-.414 0-.75.336-.75.75v8.5c0 .414.336.75.75.75s.75-.336.75-.75v-8.5c0-.414-.336-.75-.75-.75zm3.75-4v-.5h-3v.5z" fill-rule="nonzero"/></svg>
		</div>
		`;
	}
};

// Import the functions you need from the SDKs you need
import { initializeApp } from 'https://www.gstatic.com/firebasejs/10.5.2/firebase-app.js';
import { getAuth, signInAnonymously, signInWithPopup, GoogleAuthProvider } from 'https://www.gstatic.com/firebasejs/10.5.2/firebase-auth.js';

// Your web app's Firebase configuration
// For Firebase JS SDK v7.20.0 and later, measurementId is optional
const firebaseConfig = {
	apiKey: 'AIzaSyDgdYkUpixss87VF6DbAKle4LKQG_eXs_k',
	authDomain: 'bigbox-34654.firebaseapp.com',
	projectId: 'bigbox-34654',
	storageBucket: 'bigbox-34654.appspot.com',
	messagingSenderId: '441356283577',
	appId: '1:441356283577:web:cce7891fec089402bbb568',
	measurementId: 'G-361PPX2KVD'
};

// Initialize Firebase
const app = initializeApp(firebaseConfig);
const auth = getAuth(app);
let prev = auth.currentUser;

auth.onAuthStateChanged(async u => {
	if (u) {
		console.log(u);

		if (prev?.isAnonymous && !u.isAnonymous) {
			await fetch('/login', {
				method: 'POST',
				body: JSON.stringify({
					from: prev.accessToken,
					to: u.accessToken,
				}),
				headers: { 'Content-Type': 'application/json' },
			});
		}

		if (prev?.uid !== u.uid) {
			const filesRes = await fetch('/files', {
				headers: { 'Authorization': u.accessToken },
			});

			const files = await filesRes.json();
			renderFileView(files);
		}

		document.getElementById('name').innerText = u.displayName || 'Anonymous';
		document.getElementById('subtext').innerText = u.isAnonymous ? '(log in)' : '(log out)';
	} else {
		signInAnonymously(auth);
	}
	prev = u;
});

const provider = new GoogleAuthProvider();
window.signInOrOut = () => {
	if (auth.currentUser.isAnonymous) {
		signInWithPopup(auth, provider);
	} else {
		auth.signOut();
	}
};

window.deleteFile = async fileID => {
	await fetch(`/files/${fileID}`, {
		method: 'DELETE',
		headers: { 'Authorization': auth.currentUser.accessToken },
	});

	renderFileView(allFiles.filter(f => f.id !== fileID));
};

window.auth = auth;