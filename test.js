let foo = new Promise((res)=>{
    setTimeout(()=> res(true), 100);
});

let foo2 = new Promise((res)=>{
    setTimeout(()=> res(true), 150);
})

async function foo3 (){
    console.log(await Promise.all([foo, foo2]));
}

foo3();

