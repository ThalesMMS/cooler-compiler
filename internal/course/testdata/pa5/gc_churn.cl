class Node {
  value : Int;
  next : Node;
  set(v : Int, n : Node) : Node { { value <- v; next <- n; self; } };
  value() : Int { value };
  next() : Node { next };
};
class Main inherits IO {
  main() : Object {
    let keep : Node, i : Int <- 0, total : Int <- 0 in {
      while i < 2000 loop {
        let garbage : Node <- (new Node).set(i, new Node) in total <- total + garbage.value();
        if i - (i / 100) * 100 = 0 then keep <- (new Node).set(i, keep) else 0 fi;
        i <- i + 1;
      } pool;
      out_int(total); out_string(" ");
      while not isvoid keep loop { out_int(keep.value()); out_string(" "); keep <- keep.next(); } pool;
      out_string("\n");
    }
  };
};
