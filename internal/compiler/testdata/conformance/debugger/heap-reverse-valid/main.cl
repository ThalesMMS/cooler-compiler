class Parent {
  inherited : Int <- 7;
};

class Box inherits Parent {
  value : Int <- 1;
  next : Box;
  set(n : Box) : Box {{ next <- n; self; }};
  link_back(n : Box) : Box {{ next.set(n); self; }};
};

class Main {
  first : Box;
  second : Box;
  i : Int <- 0;
  main() : Object {{
    first <- new Box;
    first.set(new Box);
    second <- first.copy();
    second.link_back(first);
    while i < 5 loop i <- i + 1 pool;
    self;
  }};
};
