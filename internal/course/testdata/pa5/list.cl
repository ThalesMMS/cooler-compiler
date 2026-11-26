class List {
  isNil() : Bool { true };
  head() : Int { { abort(); 0; } };
  tail() : List { { abort(); self; } };
  cons(i : Int) : List { (new Cons).init(i, self) };
};
class Cons inherits List {
  car : Int;
  cdr : List;
  isNil() : Bool { false };
  head() : Int { car };
  tail() : List { cdr };
  init(i : Int, rest : List) : List { { car <- i; cdr <- rest; self; } };
};
class Main inherits IO {
  print(l : List) : Object {
    if l.isNil() then out_string("\n") else { out_int(l.head()); out_string(" "); print(l.tail()); } fi
  };
  main() : Object {
    let l : List <- new List, i : Int <- 0 in {
      while i < 8 loop { l <- l.cons(i * i); i <- i + 1; } pool;
      print(l);
    }
  };
};
